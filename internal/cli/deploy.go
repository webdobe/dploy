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

var deployCmd = &cobra.Command{
	Use:   "deploy <environment>",
	Short: "Run deploy steps for an environment",
	Long: `Run the configured deploy steps against the named environment.

Behavior:
  - loads config file
  - validates environment exists
  - connects locally or over SSH (SSH is stubbed)
  - changes into the configured path
  - runs deploy steps in order
  - streams output
  - exits on first failure
  - stores deploy result under .dploy/state/`,
	Args: cobra.ExactArgs(1),
	RunE: runDeploy,
}

func init() {
	rootCmd.AddCommand(deployCmd)
}

func runDeploy(cmd *cobra.Command, args []string) error {
	envName := args[0]
	log := logging.New(verbose, quiet)

	// 1. Load config.
	cfg, err := config.Load(configFile)
	if err != nil {
		return failure.WithExit(failure.ExitInvalidConfig, err)
	}

	// 2. Validate config.
	if errs := config.Validate(cfg); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(cmd.ErrOrStderr(), "  - %s\n", e)
		}
		return failure.WithExit(failure.ExitInvalidConfig, fmt.Errorf("config is invalid: %d error(s)", len(errs)))
	}

	// 3. Resolve environment.
	resolved, err := environment.Resolve(cfg, envName)
	if err != nil {
		return failure.WithExit(failure.ExitEnvironmentMissing, err)
	}

	// 4. Build the operation request.
	req := operation.Request{
		Type:        operation.TypeDeploy,
		Environment: envName,
		Class:       resolved.Class,
	}

	// 5. Trusted policy. Missing file at the default path is silent
	//    (empty policy = allow); at an explicit --policy path, Load will
	//    have surfaced the error. Either way, Evaluate is safe against
	//    an empty Policy.
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
		// Rule allowed conditionally, but dploy can't satisfy those
		// conditions yet (confirmation flags, sanitization checks, etc.
		// are future work). Fail closed rather than run without them.
		return failure.WithExit(failure.ExitPolicyDenied, &failure.PolicyError{
			Source:  pol.Source,
			Reason:  "unmet requirement(s) and no mechanism to satisfy them in this version",
			Require: decision.Requirements,
		})
	}

	// 6. Build plan.
	plan, err := planner.BuildDeploy(req, cfg, resolved)
	if err != nil {
		return failure.WithExit(failure.ExitInvalidConfig, err)
	}

	// 7. Header for humans.
	log.Info("Running deploy for environment: %s", plan.Environment)
	for _, t := range plan.Targets {
		switch t.Type {
		case config.TargetSSH:
			log.Info("Target: %s (%s)", t.Name, t.Host)
		default:
			log.Info("Target: %s (local)", t.Name)
		}
		log.Info("Path: %s", t.Path)
	}
	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}

	// 8. Execute. Step output streams to stdout so users see it live;
	//    under --quiet we suppress streaming (output is still captured
	//    into the recorded StepResult so `dploy logs` has everything).
	multiTarget := len(plan.Targets) > 1
	var stream io.Writer
	if !quiet {
		stream = cmd.OutOrStdout()
	}
	seq := executor.NewSequential(stream, func(target string, index, total int, command string) {
		if multiTarget {
			log.Step(index, total, fmt.Sprintf("[%s] %s", target, command))
		} else {
			log.Step(index, total, command)
		}
	})

	ctx := context.Background()
	result, err := seq.Execute(ctx, plan)
	if err != nil {
		return failure.WithExit(failure.ExitGeneralFailure, err)
	}

	// Attach the policy source so the recorded result has audit context.
	result.PolicySrc = pol.Source

	// 9. Record. Recording failure must not mask the primary result.
	store := state.NewFileStore(filepath.Join(".dploy", "state"))
	if recErr := store.Record(result); recErr != nil {
		log.Error("warning: failed to record state: %v", recErr)
	}

	// 10. Summarize and map status to exit code.
	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}
	switch result.Status {
	case operation.StatusSuccess:
		log.Info("Deploy succeeded")
		return nil
	case operation.StatusPartialFailure:
		return failure.WithExit(
			failure.ExitStepFailure,
			fmt.Errorf("deploy partial_failure: %d target(s) changed before a later step failed", countSuccessfulTargets(plan, result)),
		)
	case operation.StatusFailedExecution:
		return failure.WithExit(
			failure.ExitStepFailure,
			fmt.Errorf("deploy failed_execution: see logs for the failing step"),
		)
	case operation.StatusFailedResolution:
		return failure.WithExit(
			failure.ExitGeneralFailure,
			fmt.Errorf("deploy failed_resolution: could not prepare a target"),
		)
	default:
		return failure.WithExit(failure.ExitGeneralFailure, fmt.Errorf("deploy finished with status %s", result.Status))
	}
}

// countSuccessfulTargets reports how many plan targets had all their steps succeed.
// Used only for the partial_failure summary line.
func countSuccessfulTargets(plan *planner.Plan, result *operation.Result) int {
	byTarget := map[string]int{}
	failed := map[string]bool{}
	for _, s := range result.Steps {
		byTarget[s.Target]++
		if s.Status != operation.StatusSuccess {
			failed[s.Target] = true
		}
	}
	n := 0
	for _, t := range plan.Targets {
		if failed[t.Name] {
			continue
		}
		if byTarget[t.Name] == len(t.Steps) {
			n++
		}
	}
	return n
}
