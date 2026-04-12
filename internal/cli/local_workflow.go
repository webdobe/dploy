package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/webdobe/dploy/internal/executor"
	"github.com/webdobe/dploy/internal/failure"
	"github.com/webdobe/dploy/internal/logging"
	"github.com/webdobe/dploy/internal/operation"
	"github.com/webdobe/dploy/internal/planner"
	"github.com/webdobe/dploy/internal/policy"
	"github.com/webdobe/dploy/internal/state"
)

// evaluatePolicy loads the trusted policy and checks req against it.
// On deny or unmet requirements, returns a fully-wrapped failure error
// ready to bubble out of a cobra RunE. On allow, returns the loaded
// policy so callers can pass policy.Source into additional records.
//
// Shared by the single-env-local-script commands (sync, capture,
// restore); deploy/rollback go through runPipeline instead.
func evaluatePolicy(cmd *cobra.Command, log *logging.Logger, req operation.Request) (*policy.Policy, error) {
	pol, err := policy.Load(policyFile)
	if err != nil {
		return nil, failure.WithExit(failure.ExitGeneralFailure, err)
	}
	if pol.Source != "" {
		log.Debug("loaded policy from %s (%d rules)", pol.Source, len(pol.Rules))
	}
	decision := pol.Evaluate(req)
	if !decision.Allowed {
		return pol, failure.WithExit(failure.ExitPolicyDenied, &failure.PolicyError{
			Source:  pol.Source,
			Reason:  decision.Reason,
			Require: decision.Requirements,
		})
	}
	if len(decision.Unmet) > 0 {
		if hint := suggestFlagsFor(decision.Unmet); hint != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), "hint: "+hint)
		}
		return pol, failure.WithExit(failure.ExitPolicyDenied, &failure.PolicyError{
			Source:  pol.Source,
			Reason:  "unmet policy requirement(s)",
			Require: decision.Unmet,
		})
	}
	return pol, nil
}

// executeAndRecord runs a locally-targeted plan with the sequential
// executor, stamps policySrc onto the result, lets the caller attach
// op-specific fields via decorate, and persists state.
//
// Suitable for sync, capture, and restore — all of which plan a
// single local "target" and produce one Result.
func executeAndRecord(
	cmd *cobra.Command,
	log *logging.Logger,
	plan *planner.Plan,
	policySrc string,
	decorate func(*operation.Result),
) (*operation.Result, error) {
	var stream io.Writer
	if !quiet {
		stream = cmd.OutOrStdout()
	}
	seq := executor.NewSequential(stream, func(_ string, index, total int, command string) {
		log.Step(index, total, command)
	})

	result, err := seq.Execute(context.Background(), plan)
	if err != nil {
		return nil, failure.WithExit(failure.ExitGeneralFailure, err)
	}
	result.PolicySrc = policySrc
	if decorate != nil {
		decorate(result)
	}

	store := state.NewFileStore(filepath.Join(".dploy", "state"))
	if recErr := store.Record(result); recErr != nil {
		log.Error("warning: failed to record state: %v", recErr)
	}
	return result, nil
}
