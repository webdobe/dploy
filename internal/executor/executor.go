// Package executor runs planned steps against resolved targets.
//
// The executor does not decide what to run — it runs what the planner
// already scheduled. It reports every step's result honestly, including
// partial failures in multi-target plans (see FAILURE_MODEL.md).
package executor

import (
	"context"
	"io"

	"github.com/webdobe/dploy/internal/config"
	"github.com/webdobe/dploy/internal/operation"
	"github.com/webdobe/dploy/internal/planner"
)

// Runner executes one step against one target. Implementations are
// transport-specific (local shell, SSH, etc.).
type Runner interface {
	Run(ctx context.Context, target planner.TargetPlan, step planner.Step) operation.StepResult
	Close() error
}

// Executor is the top-level orchestrator that walks a Plan and records results.
type Executor interface {
	Execute(ctx context.Context, p *planner.Plan) (*operation.Result, error)
}

// RunnerFor returns an appropriate Runner for the target's type. stream
// (optional) receives live per-step output. Callers are responsible for
// calling Close on the returned runner.
func RunnerFor(t planner.TargetPlan, stream io.Writer) (Runner, error) {
	switch t.Type {
	case config.TargetLocal:
		return NewLocalRunner(stream), nil
	case config.TargetSSH:
		return NewSSHRunner(t.Host, stream), nil
	default:
		return nil, &UnsupportedTargetError{Type: t.Type}
	}
}

// UnsupportedTargetError is returned when a target type has no runner.
type UnsupportedTargetError struct {
	Type string
}

func (e *UnsupportedTargetError) Error() string {
	return "executor: unsupported target type " + e.Type
}
