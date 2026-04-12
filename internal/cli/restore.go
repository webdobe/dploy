package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

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

var (
	restoreResources []string
	restoreSnapshot  string
)

var restoreCmd = &cobra.Command{
	Use:   "restore <environment>",
	Short: "Restore a previously captured snapshot into an environment",
	Long: `Apply a previously captured snapshot to the named environment by running
that environment's restore workflow(s).

The snapshot must already exist in .dploy/snapshots/ (created by 'dploy capture').
Scripts are responsible for actually fetching the snapshot's data and loading
it into the target (download + mysql < dump.sql, untar into place, etc.).

Workflows are pulled from the environment's restore: block in dploy.yml,
keyed by resource name. Scripts run locally, with these env vars set:

  DPLOY_TARGET         name of the environment being restored into
  DPLOY_TARGET_CLASS   class of that environment
  DPLOY_RESOURCES      comma-separated list of requested resources
  DPLOY_SNAPSHOT_ID    the snapshot identifier to restore
  DPLOY_SNAPSHOT_ENV   the environment the snapshot was captured from

Examples:
  dploy restore local --snapshot production-20260412-120000-abcdef --resource database
  dploy restore staging --snapshot <id> --resource database,files`,
	Args: cobra.ExactArgs(1),
	RunE: runRestore,
}

func init() {
	rootCmd.AddCommand(restoreCmd)
	restoreCmd.Flags().StringSliceVar(&restoreResources, "resource", nil,
		"resource(s) to restore (repeatable or comma-separated); required")
	restoreCmd.Flags().StringVar(&restoreSnapshot, "snapshot", "",
		"snapshot id to restore (as produced by 'dploy capture'); required")
}

func runRestore(cmd *cobra.Command, args []string) error {
	envName := args[0]
	log := logging.New(verbose, quiet)

	if len(restoreResources) == 0 {
		return failure.WithExit(failure.ExitGeneralFailure,
			fmt.Errorf("restore requires --resource (e.g. --resource database)"))
	}
	if restoreSnapshot == "" {
		return failure.WithExit(failure.ExitGeneralFailure,
			fmt.Errorf("restore requires --snapshot <id>"))
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

	// 2. Resolve the target environment.
	target, err := environment.Resolve(cfg, envName)
	if err != nil {
		return failure.WithExit(failure.ExitEnvironmentMissing, err)
	}

	// 3. Locate the snapshot. We search across envs since the user may
	//    be restoring a prod snapshot into a local env (the common case).
	snapStore := state.NewFileSnapshotStore(filepath.Join(".dploy", "snapshots"))
	snap, err := findSnapshot(snapStore, cfg, restoreSnapshot)
	if err != nil {
		return failure.WithExit(failure.ExitGeneralFailure, err)
	}

	// 4. Build request.
	req := operation.Request{
		Type:        operation.TypeRestore,
		Environment: envName,
		Class:       target.Class,
		Resources:   restoreResources,
		Satisfied:   collectSatisfiedRequirements(),
	}

	// 5. Trusted policy.
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

	// 6. Plan.
	plan, err := planner.BuildRestore(req, cfg, target, snap.ID, snap.Env)
	if err != nil {
		return failure.WithExit(failure.ExitInvalidConfig, err)
	}

	// 7. Header.
	log.Info("Restoring snapshot %s (from %s) into %s (resources: %v)", snap.ID, snap.Env, envName, restoreResources)
	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}

	// 8. Execute.
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
	result.Resources = restoreResources
	result.SnapshotID = snap.ID

	// 9. Record state.
	store := state.NewFileStore(filepath.Join(".dploy", "state"))
	if recErr := store.Record(result); recErr != nil {
		log.Error("warning: failed to record state: %v", recErr)
	}

	// 10. Summarize.
	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}
	switch result.Status {
	case operation.StatusSuccess:
		log.Info("Restore succeeded (snapshot: %s)", snap.ID)
		return nil
	case operation.StatusPartialFailure:
		return failure.WithExit(
			failure.ExitStepFailure,
			fmt.Errorf("restore partial_failure: some resources restored before a later step failed (snapshot: %s)", snap.ID),
		)
	case operation.StatusFailedExecution:
		return failure.WithExit(
			failure.ExitStepFailure,
			fmt.Errorf("restore failed_execution: see logs for the failing step (snapshot: %s)", snap.ID),
		)
	default:
		return failure.WithExit(failure.ExitGeneralFailure, fmt.Errorf("restore finished with status %s (snapshot: %s)", result.Status, snap.ID))
	}
}

// findSnapshot looks up a snapshot by ID. The snapshot store is keyed
// by (env, id), but restore callers typically know the ID (e.g. from
// 'dploy capture' output) without remembering the origin env. We scan
// each configured environment so the user can just paste the ID.
func findSnapshot(store state.SnapshotStore, cfg *config.Config, id string) (*state.Snapshot, error) {
	for envName := range cfg.Environments {
		snap, err := store.Get(envName, id)
		if err == nil {
			return snap, nil
		}
		if !errors.Is(err, state.ErrSnapshotNotFound) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("snapshot %q not found in .dploy/snapshots/", id)
}
