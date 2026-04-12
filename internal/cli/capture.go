package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/webdobe/dploy/internal/config"
	"github.com/webdobe/dploy/internal/environment"
	"github.com/webdobe/dploy/internal/executor"
	"github.com/webdobe/dploy/internal/failure"
	"github.com/webdobe/dploy/internal/logging"
	"github.com/webdobe/dploy/internal/operation"
	"github.com/webdobe/dploy/internal/planner"
	"github.com/webdobe/dploy/internal/policy"
	"github.com/webdobe/dploy/internal/state"
)

var captureResources []string

var captureCmd = &cobra.Command{
	Use:   "capture <environment>",
	Short: "Capture a point-in-time snapshot of environment resources",
	Long: `Run the configured capture workflow(s) to produce a point-in-time
snapshot of the named environment's resources.

Workflows are pulled from the environment's capture: block in dploy.yml,
keyed by resource name. Scripts run locally, with these env vars set:

  DPLOY_SOURCE         name of the environment being captured
  DPLOY_SOURCE_CLASS   class of that environment
  DPLOY_RESOURCES      comma-separated list of requested resources
  DPLOY_SNAPSHOT_ID    the snapshot identifier dploy generated for this run

Scripts are responsible for actually persisting the captured data (mysqldump,
tar, upload to object storage, etc.). dploy records the snapshot metadata
(id, env, resources, status, sanitization marker) under .dploy/snapshots/.

Examples:
  dploy capture production --resource database
  dploy capture production --resource database,files --sanitized`,
	Args: cobra.ExactArgs(1),
	RunE: runCapture,
}

func init() {
	rootCmd.AddCommand(captureCmd)
	captureCmd.Flags().StringSliceVar(&captureResources, "resource", nil,
		"resource(s) to capture (repeatable or comma-separated); required")
}

func runCapture(cmd *cobra.Command, args []string) error {
	envName := args[0]
	log := logging.New(verbose, quiet)

	if len(captureResources) == 0 {
		return failure.WithExit(failure.ExitGeneralFailure,
			fmt.Errorf("capture requires --resource (e.g. --resource database)"))
	}

	// 1. Load + validate config.
	cfg, err := config.Load(configFile)
	if err != nil {
		return failure.WithExit(failure.ExitInvalidConfig, err)
	}
	if errs := config.Validate(cfg); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(cmd.ErrOrStderr(), "  - %s\n", e)
		}
		return failure.WithExit(failure.ExitInvalidConfig, fmt.Errorf("config is invalid: %d error(s)", len(errs)))
	}

	// 2. Resolve the environment.
	source, err := environment.Resolve(cfg, envName)
	if err != nil {
		return failure.WithExit(failure.ExitEnvironmentMissing, err)
	}

	// 3. Build request and generate the snapshot ID up front so it can
	//    flow into scripts as DPLOY_SNAPSHOT_ID and also end up on the
	//    recorded Result + Snapshot metadata.
	startedAt := time.Now()
	snapshotID := state.NewSnapshotID(envName, startedAt)

	req := operation.Request{
		Type:        operation.TypeCapture,
		Environment: envName,
		Class:       source.Class,
		Resources:   captureResources,
		Satisfied:   collectSatisfiedRequirements(),
	}

	// 4. Trusted policy.
	pol, err := policy.Load(policyFile)
	if err != nil {
		return failure.WithExit(failure.ExitGeneralFailure, err)
	}
	if pol.Source != "" {
		log.Debug("loaded policy from %s (%d rules)", pol.Source, len(pol.Rules))
	}
	decision := pol.Evaluate(req)
	if !decision.Allowed {
		return failure.WithExit(failure.ExitPolicyDenied, &failure.PolicyError{
			Source:  pol.Source,
			Reason:  decision.Reason,
			Require: decision.Requirements,
		})
	}
	if len(decision.Unmet) > 0 {
		if hint := suggestFlagsFor(decision.Unmet); hint != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), "hint: "+hint)
		}
		return failure.WithExit(failure.ExitPolicyDenied, &failure.PolicyError{
			Source:  pol.Source,
			Reason:  "unmet policy requirement(s)",
			Require: decision.Unmet,
		})
	}

	// 5. Plan.
	plan, err := planner.BuildCapture(req, cfg, source, snapshotID)
	if err != nil {
		return failure.WithExit(failure.ExitInvalidConfig, err)
	}

	// 6. Header.
	log.Info("Running capture for %s (snapshot %s, resources: %v)", envName, snapshotID, captureResources)
	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}

	// 7. Execute.
	var stream io.Writer
	if !quiet {
		stream = cmd.OutOrStdout()
	}
	seq := executor.NewSequential(stream, func(_ string, index, total int, command string) {
		log.Step(index, total, command)
	})

	ctx := context.Background()
	result, err := seq.Execute(ctx, plan)
	if err != nil {
		return failure.WithExit(failure.ExitGeneralFailure, err)
	}
	result.PolicySrc = pol.Source
	result.Resources = captureResources
	result.SnapshotID = snapshotID
	// StartedAt already set by Execute; overwrite with our pre-exec time
	// so the snapshot id and the record timestamps line up.
	result.StartedAt = startedAt

	// 8. Record state (per-env latest-op).
	store := state.NewFileStore(filepath.Join(".dploy", "state"))
	if recErr := store.Record(result); recErr != nil {
		log.Error("warning: failed to record state: %v", recErr)
	}

	// 9. Record snapshot metadata for any operation that actually ran.
	//    Pre-execution failures (validate/policy) return earlier, so if
	//    we got here we at least attempted work — users deserve a record.
	snapStore := state.NewFileSnapshotStore(filepath.Join(".dploy", "snapshots"))
	snap := &state.Snapshot{
		ID:         snapshotID,
		Env:        envName,
		Class:      source.Class,
		Resources:  captureResources,
		Status:     result.Status,
		Sanitized:  sanitizedFlag,
		CreatedAt:  startedAt,
		FinishedAt: result.FinishedAt,
		PolicySrc:  pol.Source,
	}
	if recErr := snapStore.Record(snap); recErr != nil {
		log.Error("warning: failed to record snapshot metadata: %v", recErr)
	}

	// 10. Summarize.
	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}
	switch result.Status {
	case operation.StatusSuccess:
		log.Info("Capture succeeded (snapshot: %s)", snapshotID)
		return nil
	case operation.StatusPartialFailure:
		return failure.WithExit(
			failure.ExitStepFailure,
			fmt.Errorf("capture partial_failure: some resources captured before a later step failed (snapshot: %s)", snapshotID),
		)
	case operation.StatusFailedExecution:
		return failure.WithExit(
			failure.ExitStepFailure,
			fmt.Errorf("capture failed_execution: see logs for the failing step (snapshot: %s)", snapshotID),
		)
	default:
		return failure.WithExit(failure.ExitGeneralFailure, fmt.Errorf("capture finished with status %s (snapshot: %s)", result.Status, snapshotID))
	}
}
