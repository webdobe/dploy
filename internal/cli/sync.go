package cli

import (
	"context"
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

var syncResources []string

var syncCmd = &cobra.Command{
	Use:   "sync <source> <target>",
	Short: "Sync resources from source environment to target environment",
	Long: `Sync resources between environments using the source env's data: workflows.

Workflows are looked up by resource name in the source environment's
data: block. Scripts run locally (on the machine invoking dploy), with
these env vars exposed:

  DPLOY_SOURCE         name of the source environment
  DPLOY_TARGET         name of the target environment
  DPLOY_SOURCE_CLASS   class of the source environment
  DPLOY_TARGET_CLASS   class of the target environment
  DPLOY_RESOURCES      comma-separated list of requested resources

Scripts are responsible for their own remote access (mysqldump -h...,
rsync, etc.) — dploy does not SSH for sync in v1.

Examples:
  dploy env sync production local --resource database
  dploy env sync production local --resource database,files`,
	Args: cobra.ExactArgs(2),
	RunE: runSync,
}

func init() {
	envCmd.AddCommand(syncCmd)
	syncCmd.Flags().StringSliceVar(&syncResources, "resource", nil,
		"resource(s) to sync (repeatable or comma-separated); required")
}

func runSync(cmd *cobra.Command, args []string) error {
	sourceName, targetName := args[0], args[1]
	log := logging.New(verbose, quiet)

	if len(syncResources) == 0 {
		return failure.WithExit(failure.ExitGeneralFailure,
			fmt.Errorf("sync requires --resource (e.g. --resource database)"))
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

	// 2. Resolve both environments.
	source, err := environment.Resolve(cfg, sourceName)
	if err != nil {
		return failure.WithExit(failure.ExitEnvironmentMissing, fmt.Errorf("source env: %w", err))
	}
	target, err := environment.Resolve(cfg, targetName)
	if err != nil {
		return failure.WithExit(failure.ExitEnvironmentMissing, fmt.Errorf("target env: %w", err))
	}

	// 3. Build the operation request (source+target, not single env).
	req := operation.Request{
		Type:        operation.TypeSync,
		SourceEnv:   sourceName,
		SourceClass: source.Class,
		TargetEnv:   targetName,
		TargetClass: target.Class,
		Resources:   syncResources,
	}

	// 4. Trusted policy. Sync is especially sensitive — policy rules
	//    typically gate source→target direction, sanitization, etc.
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
	if len(decision.Requirements) > 0 {
		return failure.WithExit(failure.ExitPolicyDenied, &failure.PolicyError{
			Source:  pol.Source,
			Reason:  "unmet requirement(s) and no mechanism to satisfy them in this version",
			Require: decision.Requirements,
		})
	}

	// 5. Build the plan.
	plan, err := planner.BuildSync(req, cfg, source, target)
	if err != nil {
		return failure.WithExit(failure.ExitInvalidConfig, err)
	}

	// 6. Header for humans.
	log.Info("Running sync: %s → %s (resources: %v)", sourceName, targetName, syncResources)
	log.Info("Running locally in: %s", plan.Targets[0].Path)
	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}

	// 7. Execute. Sync has a single local "target" so we don't prefix
	//    step announcements with a target name.
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
	// The plan already set Environment = target.Name so status/logs
	// lookups work. Attach source/target/resources to the record for
	// audit visibility.
	result.SourceEnv = sourceName
	result.TargetEnv = targetName
	result.Resources = syncResources

	// 8. Record.
	store := state.NewFileStore(filepath.Join(".dploy", "state"))
	if recErr := store.Record(result); recErr != nil {
		log.Error("warning: failed to record state: %v", recErr)
	}

	// 9. Summarize and map status to exit code.
	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}
	switch result.Status {
	case operation.StatusSuccess:
		log.Info("Sync succeeded")
		return nil
	case operation.StatusPartialFailure:
		return failure.WithExit(
			failure.ExitStepFailure,
			fmt.Errorf("sync partial_failure: some steps completed before a later step failed"),
		)
	case operation.StatusFailedExecution:
		return failure.WithExit(
			failure.ExitStepFailure,
			fmt.Errorf("sync failed_execution: see logs for the failing step"),
		)
	default:
		return failure.WithExit(failure.ExitGeneralFailure, fmt.Errorf("sync finished with status %s", result.Status))
	}
}
