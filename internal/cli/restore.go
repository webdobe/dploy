package cli

import (
	"context"
	"errors"
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
	"github.com/webdobe/dploy/internal/state"
)

var restoreResourceFlag []string

var restoreCmd = &cobra.Command{
	Use:   "restore <snapshot-id> <environment>",
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
  dploy restore production-20260412-120000-abcdef local --resource database
  dploy restore <snapshot-id> staging --resource database,files`,
	Args: cobra.ExactArgs(2),
	RunE: runRestore,
}

func init() {
	rootCmd.AddCommand(restoreCmd)
	restoreCmd.Flags().StringSliceVar(&restoreResourceFlag, "resource", nil,
		"resource(s) to restore (repeatable or comma-separated); auto-picks when exactly one is defined")
}

func runRestore(cmd *cobra.Command, args []string) error {
	snapshotID, envName := args[0], args[1]
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

	// 2. Resolve the target environment.
	target, err := environment.Resolve(cfg, envName)
	if err != nil {
		return failure.WithExit(failure.ExitEnvironmentMissing, err)
	}

	// 3. Resolve --resource (auto-pick when exactly one defined).
	resources, err := resolveResources(restoreResourceFlag, cfg.Environments[envName].Restore, "restore")
	if err != nil {
		return failure.WithExit(failure.ExitGeneralFailure, err)
	}

	// 4. Locate the snapshot. We search across envs since the user may
	//    be restoring a prod snapshot into a local env (the common case).
	snapStore := state.NewFileSnapshotStore(filepath.Join(".dploy", "snapshots"))
	snap, err := findSnapshot(snapStore, cfg, snapshotID)
	if err != nil {
		return failure.WithExit(failure.ExitGeneralFailure, err)
	}

	// 5. Take a safety snapshot of the target's current state before we
	//    overwrite it. Runs the target env's capture workflow for any of
	//    the requested resources that have one defined. If the target
	//    defines no matching capture, safety is quietly skipped — users
	//    who want it can add a capture: block to the target env.
	safetyID, err := takeSafetySnapshot(cmd, log, cfg, target, resources)
	if err != nil {
		return failure.WithExit(failure.ExitGeneralFailure, fmt.Errorf("safety snapshot failed: %w", err))
	}

	// 6. Build request.
	req := operation.Request{
		Type:        operation.TypeRestore,
		Environment: envName,
		Class:       target.Class,
		Resources:   resources,
		Satisfied:   collectSatisfiedRequirements(),
	}

	// 7. Trusted policy.
	pol, err := evaluatePolicy(cmd, log, req)
	if err != nil {
		return err
	}

	// 8. Plan.
	plan, err := planner.BuildRestore(req, cfg, target, snap.ID, snap.Env)
	if err != nil {
		return failure.WithExit(failure.ExitInvalidConfig, err)
	}

	// 9. Header.
	log.Info("Restoring snapshot %s (from %s) into %s (resources: %v)", snap.ID, snap.Env, envName, resources)
	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}

	// 10. Execute + record.
	result, err := executeAndRecord(cmd, log, plan, pol.Source, func(r *operation.Result) {
		r.Resources = resources
		r.SnapshotID = snap.ID
	})
	if err != nil {
		return err
	}

	// 11. Summarize.
	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}
	switch result.Status {
	case operation.StatusSuccess:
		log.Info("Restore succeeded (snapshot: %s)", snap.ID)
		return nil
	case operation.StatusPartialFailure:
		printSafetyRevertHint(cmd, safetyID, envName, resources)
		return failure.WithExit(
			failure.ExitStepFailure,
			fmt.Errorf("restore partial_failure: some resources restored before a later step failed (snapshot: %s)", snap.ID),
		)
	case operation.StatusFailedExecution:
		printSafetyRevertHint(cmd, safetyID, envName, resources)
		return failure.WithExit(
			failure.ExitStepFailure,
			fmt.Errorf("restore failed_execution: see logs for the failing step (snapshot: %s)", snap.ID),
		)
	default:
		printSafetyRevertHint(cmd, safetyID, envName, resources)
		return failure.WithExit(failure.ExitGeneralFailure, fmt.Errorf("restore finished with status %s (snapshot: %s)", result.Status, snap.ID))
	}
}

// takeSafetySnapshot runs the target environment's capture workflow for
// any of the requested resources it defines. Returns the safety snapshot
// ID, or "" if the target has no matching capture workflow (safety is a
// best-effort; absence of a capture block is a reason to skip, not fail).
func takeSafetySnapshot(cmd *cobra.Command, log *logging.Logger, cfg *config.Config, target *environment.Resolved, resources []string) (string, error) {
	envCfg := cfg.Environments[target.Name]
	var covered []string
	for _, r := range resources {
		if _, ok := envCfg.Capture[r]; ok {
			covered = append(covered, r)
		}
	}
	if len(covered) == 0 {
		log.Info("Safety snapshot skipped: no capture workflow on %q for %v", target.Name, resources)
		return "", nil
	}

	startedAt := time.Now()
	safetyID := state.NewSnapshotID("safety-"+target.Name, startedAt)

	req := operation.Request{
		Type:        operation.TypeCapture,
		Environment: target.Name,
		Class:       target.Class,
		Resources:   covered,
	}
	plan, err := planner.BuildCapture(req, cfg, target, safetyID)
	if err != nil {
		return "", err
	}

	log.Info("Safety snapshot: capturing %v on %q as %s", covered, target.Name, safetyID)
	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}

	var stream io.Writer
	if !quiet {
		stream = cmd.OutOrStdout()
	}
	seq := executor.NewSequential(stream, func(targetName string, index, total int, command string) {
		log.Step(index, total, fmt.Sprintf("[safety] %s", command))
	})
	result, err := seq.Execute(context.Background(), plan)
	if err != nil {
		return "", err
	}
	if result.Status != operation.StatusSuccess {
		return "", fmt.Errorf("safety capture status %s", result.Status)
	}

	snapStore := state.NewFileSnapshotStore(filepath.Join(".dploy", "snapshots"))
	if recErr := snapStore.Record(&state.Snapshot{
		ID:         safetyID,
		Env:        target.Name,
		Class:      target.Class,
		Resources:  covered,
		Status:     result.Status,
		CreatedAt:  startedAt,
		FinishedAt: result.FinishedAt,
	}); recErr != nil {
		log.Error("warning: failed to record safety snapshot metadata: %v", recErr)
	}

	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return safetyID, nil
}

// printSafetyRevertHint shows the command to revert if the main restore
// fails and we captured a safety snapshot beforehand.
func printSafetyRevertHint(cmd *cobra.Command, safetyID, envName string, resources []string) {
	if quiet || safetyID == "" {
		return
	}
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "Target may be in a partial state. To revert:")
	fmt.Fprintf(cmd.OutOrStdout(), "  dploy restore %s %s --resource %s\n", safetyID, envName, joinResources(resources))
}

func joinResources(resources []string) string {
	if len(resources) == 0 {
		return ""
	}
	out := resources[0]
	for _, r := range resources[1:] {
		out += "," + r
	}
	return out
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
