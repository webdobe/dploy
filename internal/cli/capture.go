package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/webdobe/dploy/internal/config"
	"github.com/webdobe/dploy/internal/environment"
	"github.com/webdobe/dploy/internal/failure"
	"github.com/webdobe/dploy/internal/logging"
	"github.com/webdobe/dploy/internal/operation"
	"github.com/webdobe/dploy/internal/planner"
	"github.com/webdobe/dploy/internal/state"
)

var captureResourceFlag []string

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
	captureCmd.Flags().StringSliceVar(&captureResourceFlag, "resource", nil,
		"resource(s) to capture (repeatable or comma-separated); auto-picks when exactly one is defined")
}

// printRestoreHint shows the exact command needed to restore this snapshot.
// If exactly one other environment has a matching restore workflow for the
// captured resources, suggest it by name; otherwise use a placeholder.
func printRestoreHint(cmd *cobra.Command, cfg *config.Config, sourceEnv, snapshotID string, resources []string) {
	if quiet {
		return
	}
	var candidates []string
	for name, env := range cfg.Environments {
		if name == sourceEnv {
			continue
		}
		for _, r := range resources {
			if _, ok := env.Restore[r]; ok {
				candidates = append(candidates, name)
				break
			}
		}
	}
	sort.Strings(candidates)

	target := "<target-env>"
	if len(candidates) == 1 {
		target = candidates[0]
	}

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "To restore:")
	fmt.Fprintf(cmd.OutOrStdout(), "  dploy restore %s %s --resource %s\n", snapshotID, target, strings.Join(resources, ","))
	if len(candidates) > 1 {
		fmt.Fprintf(cmd.OutOrStdout(), "  (candidates: %s)\n", strings.Join(candidates, ", "))
	}
}

func runCapture(cmd *cobra.Command, args []string) error {
	envName := args[0]
	log := logging.New(verbose, quiet)

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

	// 3. Resolve --resource (auto-pick when exactly one defined).
	resources, err := resolveResources(captureResourceFlag, cfg.Environments[envName].Capture, "capture")
	if err != nil {
		return failure.WithExit(failure.ExitGeneralFailure, err)
	}

	// 4. Build request and generate the snapshot ID up front so it can
	//    flow into scripts as DPLOY_SNAPSHOT_ID and also end up on the
	//    recorded Result + Snapshot metadata.
	startedAt := time.Now()
	snapshotID := state.NewSnapshotID(envName, startedAt)

	req := operation.Request{
		Type:        operation.TypeCapture,
		Environment: envName,
		Class:       source.Class,
		Resources:   resources,
		Satisfied:   collectSatisfiedRequirements(),
	}

	// 4. Trusted policy.
	pol, err := evaluatePolicy(cmd, log, req)
	if err != nil {
		return err
	}

	// 5. Plan.
	plan, err := planner.BuildCapture(req, cfg, source, snapshotID)
	if err != nil {
		return failure.WithExit(failure.ExitInvalidConfig, err)
	}

	// 6. Header.
	log.Info("Running capture for %s (snapshot %s, resources: %v)", envName, snapshotID, resources)
	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}

	// 7. Execute + record. StartedAt already set by Execute; overwrite
	//    with our pre-exec time so the snapshot id and the record
	//    timestamps line up.
	result, err := executeAndRecord(cmd, log, plan, pol.Source, func(r *operation.Result) {
		r.Resources = resources
		r.SnapshotID = snapshotID
		r.StartedAt = startedAt
	})
	if err != nil {
		return err
	}

	// 8. Record snapshot metadata for any operation that actually ran.
	//    Pre-execution failures (validate/policy) return earlier, so if
	//    we got here we at least attempted work — users deserve a record.
	snapStore := state.NewFileSnapshotStore(filepath.Join(".dploy", "snapshots"))
	snap := &state.Snapshot{
		ID:         snapshotID,
		Env:        envName,
		Class:      source.Class,
		Resources:  resources,
		Status:     result.Status,
		Sanitized:  sanitizedFlag,
		CreatedAt:  startedAt,
		FinishedAt: result.FinishedAt,
		PolicySrc:  pol.Source,
	}
	if recErr := snapStore.Record(snap); recErr != nil {
		log.Error("warning: failed to record snapshot metadata: %v", recErr)
	}

	// 9. Summarize.
	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}
	switch result.Status {
	case operation.StatusSuccess:
		log.Info("Capture succeeded (snapshot: %s)", snapshotID)
		printRestoreHint(cmd, cfg, envName, snapshotID, resources)
		return nil
	case operation.StatusPartialFailure:
		return failure.WithExit(
			failure.ExitStepFailure,
			fmt.Errorf("capture partial_failure: some resources captured before a later step failed (snapshot: %s)%s", snapshotID, failedStepSuffix(result)),
		)
	case operation.StatusFailedExecution:
		return failure.WithExit(
			failure.ExitStepFailure,
			fmt.Errorf("capture failed_execution (snapshot: %s)%s", snapshotID, failedStepSuffix(result)),
		)
	default:
		return failure.WithExit(failure.ExitGeneralFailure, fmt.Errorf("capture finished with status %s (snapshot: %s)", result.Status, snapshotID))
	}
}
