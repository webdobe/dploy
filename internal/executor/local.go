package executor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/webdobe/dploy/internal/operation"
	"github.com/webdobe/dploy/internal/planner"
)

// LocalRunner executes steps on the local machine via /bin/sh -c.
//
// Output is written live to the stream writer (when non-nil) and also
// captured into the returned StepResult.Output so the state record has
// the full output regardless of whether the caller was watching.
type LocalRunner struct {
	stream io.Writer
}

// NewLocalRunner constructs a LocalRunner. If stream is non-nil, each
// step's combined stdout/stderr is copied to it as the step runs.
func NewLocalRunner(stream io.Writer) *LocalRunner {
	return &LocalRunner{stream: stream}
}

// Run executes step.Command in target.Path on the local host.
func (r *LocalRunner) Run(ctx context.Context, target planner.TargetPlan, step planner.Step) operation.StepResult {
	start := time.Now()

	var buf bytes.Buffer
	var out io.Writer = &buf
	if r.stream != nil {
		out = io.MultiWriter(r.stream, &buf)
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", step.Command)
	cmd.Dir = target.Path
	cmd.Stdout = out
	cmd.Stderr = out
	if len(target.Env) > 0 {
		// Merge target.Env onto the parent process environment rather
		// than replacing it — scripts almost always need PATH, HOME,
		// and similar to function.
		cmd.Env = os.Environ()
		for k, v := range target.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	err := cmd.Run()

	result := operation.StepResult{
		Index:    step.Index,
		Command:  step.Command,
		Target:   target.Name,
		Output:   buf.String(),
		Duration: time.Since(start),
	}

	if err != nil {
		result.Status = operation.StatusFailedExecution
		result.Error = err.Error()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
		return result
	}

	result.Status = operation.StatusSuccess
	return result
}

// Close is a no-op for LocalRunner.
func (r *LocalRunner) Close() error { return nil }
