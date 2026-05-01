package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

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

// pipelineOp captures the per-operation parts of runPipeline: the
// request type, display verb, plan builder, and the exit code to use
// when the plan builder returns an error.
//
// Deploy, rollback, and future env-sync all follow the same lifecycle
// (request → resolve → validate → plan → execute → record); what
// differs between them is exactly these fields.
type pipelineOp struct {
	opType     operation.Type
	verb       string // "Deploy", "Rollback", "Sync", ...
	build      func(req operation.Request, cfg *config.Config, resolved *environment.Resolved) (*planner.Plan, error)
	noPlanExit int
}

// runPipeline is the shared lifecycle for single-environment operations.
// See OPERATION_MODEL.md.
func runPipeline(cmd *cobra.Command, envName string, op pipelineOp) error {
	log := logging.New(verbose, quiet)
	lowerVerb := strings.ToLower(op.verb)

	// Load + validate config.
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

	// Resolve environment.
	resolved, err := environment.Resolve(cfg, envName)
	if err != nil {
		return failure.WithExit(failure.ExitEnvironmentMissing, err)
	}

	// Build the operation request.
	req := operation.Request{
		Type:        op.opType,
		Environment: envName,
		Class:       resolved.Class,
		Satisfied:   collectSatisfiedRequirements(),
	}

	// Trusted policy. Missing default path is silent (empty policy = allow);
	// an explicit --policy path that fails to parse will error here.
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

	// Build plan.
	plan, err := op.build(req, cfg, resolved)
	if err != nil {
		return failure.WithExit(op.noPlanExit, err)
	}

	// Header for humans.
	log.Info("Running %s for environment: %s", lowerVerb, plan.Environment)
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

	// Execute. Under --quiet we suppress streaming; the output is still
	// captured into StepResult so `dploy logs` has everything.
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
	result.PolicySrc = pol.Source

	// Record. Recording failure must not mask the primary result.
	store := state.NewFileStore(filepath.Join(".dploy", "state"))
	if recErr := store.Record(result); recErr != nil {
		log.Error("warning: failed to record state: %v", recErr)
	}

	// Summarize and map status to exit code.
	if !quiet {
		fmt.Fprintln(cmd.OutOrStdout())
	}
	switch result.Status {
	case operation.StatusSuccess:
		log.Info("%s succeeded", op.verb)
		if op.opType == operation.TypeDeploy {
			printNotes(cmd.OutOrStdout(), cfg.Environments[envName].Notes)
		}
		return nil
	case operation.StatusPartialFailure:
		return failure.WithExit(
			failure.ExitStepFailure,
			fmt.Errorf("%s partial_failure: %d target(s) changed before a later step failed%s", lowerVerb, countSuccessfulTargets(plan, result), failedStepSuffix(result)),
		)
	case operation.StatusFailedExecution:
		return failure.WithExit(
			failure.ExitStepFailure,
			fmt.Errorf("%s failed_execution%s", lowerVerb, failedStepSuffix(result)),
		)
	case operation.StatusFailedResolution:
		return failure.WithExit(
			failure.ExitGeneralFailure,
			fmt.Errorf("%s failed_resolution: could not prepare a target", lowerVerb),
		)
	default:
		return failure.WithExit(failure.ExitGeneralFailure, fmt.Errorf("%s finished with status %s", lowerVerb, result.Status))
	}
}

// printNotes writes environment notes after a successful deploy. Notes are
// guidance only — never executed — and are suppressed under --quiet.
func printNotes(w io.Writer, notes []string) {
	if quiet || len(notes) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Notes:")
	for _, n := range notes {
		fmt.Fprintf(w, "  - %s\n", n)
	}
}

// countSuccessfulTargets reports how many plan targets had all their steps succeed.
// Used for the partial_failure summary line.
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
