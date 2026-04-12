package executor

import (
	"context"
	"io"
	"time"

	"github.com/webdobe/dploy/internal/operation"
	"github.com/webdobe/dploy/internal/planner"
)

// Sequential executes a plan one target at a time, one step at a time.
//
// On any step failure it stops that target and skips remaining targets.
// If earlier targets completed fully before the failure, the result is
// partial_failure. If no target completed fully, the result is
// failed_execution. See FAILURE_MODEL.md.
type Sequential struct {
	stream   io.Writer
	announce Announcer
}

// Announcer is invoked just before each step runs, giving the caller
// (typically the CLI) a chance to print a "[i/n] command" header line.
// index is 1-based; total is the number of steps for this target.
type Announcer func(target string, index, total int, command string)

// NewSequential constructs a Sequential executor.
//
// stream (optional) is the destination for live step output.
// announce (optional) is called before each step executes.
func NewSequential(stream io.Writer, announce Announcer) *Sequential {
	return &Sequential{stream: stream, announce: announce}
}

// Execute runs the plan. A nil error means execution completed; inspect
// Result.Status to see whether it succeeded. A non-nil error means
// execution could not even start (e.g. nil plan).
func (s *Sequential) Execute(ctx context.Context, p *planner.Plan) (*operation.Result, error) {
	result := &operation.Result{
		Type:        p.Operation,
		Environment: p.Environment,
		Artifact:    p.Artifact,
		StartedAt:   time.Now(),
	}

	totalTargets := len(p.Targets)
	fullyCompleted := 0
	stop := false

	for _, target := range p.Targets {
		if stop {
			break
		}

		runner, err := RunnerFor(target, s.stream)
		if err != nil {
			// Runner construction failure means we never touched this
			// target. Record it as a resolution failure for visibility
			// and stop the run.
			result.Steps = append(result.Steps, operation.StepResult{
				Target:   target.Name,
				Status:   operation.StatusFailedResolution,
				ExitCode: -1,
				Error:    err.Error(),
			})
			stop = true
			break
		}

		targetFailed := false
		for i, step := range target.Steps {
			if s.announce != nil {
				s.announce(target.Name, i+1, len(target.Steps), step.Command)
			}

			stepResult := runner.Run(ctx, target, step)
			result.Steps = append(result.Steps, stepResult)

			if stepResult.Status != operation.StatusSuccess {
				targetFailed = true
				stop = true
				break
			}
		}

		// Close the runner regardless of success; a close error is not
		// fatal to the primary result.
		_ = runner.Close()

		if !targetFailed {
			fullyCompleted++
		}
	}

	result.FinishedAt = time.Now()

	switch {
	case totalTargets == 0:
		// Defensive: planner rejects empty plans, so this shouldn't
		// happen. Treat as failed_execution rather than success.
		result.Status = operation.StatusFailedExecution
	case fullyCompleted == totalTargets:
		result.Status = operation.StatusSuccess
	case fullyCompleted == 0:
		result.Status = operation.StatusFailedExecution
	default:
		result.Status = operation.StatusPartialFailure
	}

	return result, nil
}

// Compile-time check that Sequential satisfies Executor.
var _ Executor = (*Sequential)(nil)
