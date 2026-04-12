// Package failure defines CLI exit codes and lifecycle error types.
//
// Operation result states live on operation.ResultStatus; this package
// maps those states (and related errors) to the exit codes in CLI_SPEC.md
// and provides typed errors for the lifecycle stages.
package failure

import "fmt"

// Exit codes. These must remain stable once released.
const (
	ExitSuccess           = 0
	ExitGeneralFailure    = 1
	ExitInvalidConfig     = 2
	ExitEnvironmentMissing = 3
	ExitConnectionFailure = 4
	ExitStepFailure       = 5
	ExitRollbackUnavail   = 6
	ExitPolicyDenied      = 7
)

// ValidationError signals pre-execution config/scope validation failure.
// No execution has occurred; no side effects.
type ValidationError struct {
	Errors []error
}

func (e *ValidationError) Error() string {
	if len(e.Errors) == 1 {
		return "validation failed: " + e.Errors[0].Error()
	}
	return fmt.Sprintf("validation failed: %d error(s)", len(e.Errors))
}

// ResolutionError signals that a required reference (config include,
// artifact, workflow) could not be resolved. No execution has occurred.
type ResolutionError struct {
	What string
	Err  error
}

func (e *ResolutionError) Error() string {
	return fmt.Sprintf("resolution failed for %s: %v", e.What, e.Err)
}

func (e *ResolutionError) Unwrap() error { return e.Err }

// PolicyError signals a trusted-policy denial. No execution has occurred.
type PolicyError struct {
	Source  string
	Reason  string
	Require []string
}

func (e *PolicyError) Error() string {
	msg := "policy denied"
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	if len(e.Require) > 0 {
		msg += " (requires: "
		for i, r := range e.Require {
			if i > 0 {
				msg += ", "
			}
			msg += r
		}
		msg += ")"
	}
	if e.Source != "" {
		msg += " [source: " + e.Source + "]"
	}
	return msg
}

// ConnectionError signals inability to reach a target before any step
// ran on it. May occur before the first step or mid-operation for later
// targets in a multi-host plan.
type ConnectionError struct {
	Target string
	Host   string
	Err    error
}

func (e *ConnectionError) Error() string {
	return fmt.Sprintf("connection to target %s (%s) failed: %v", e.Target, e.Host, e.Err)
}

func (e *ConnectionError) Unwrap() error { return e.Err }

// StepError signals a step exited non-zero or otherwise failed.
// Execution started, so partial side effects are possible.
type StepError struct {
	Step     int
	Command  string
	Target   string
	ExitCode int
	Err      error
}

func (e *StepError) Error() string {
	return fmt.Sprintf("step %d on %s failed (exit %d): %s", e.Step, e.Target, e.ExitCode, e.Command)
}

func (e *StepError) Unwrap() error { return e.Err }

// ExitCodeError wraps an error with a specific CLI exit code. cmd/dploy/main.go
// checks for this type (via errors.As) to decide the process exit status.
// Use WithExit to construct.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit %d", e.Code)
}

func (e *ExitCodeError) Unwrap() error { return e.Err }

// WithExit wraps err with a CLI exit code. Returns nil if err is nil so
// callers can write `return failure.WithExit(code, someCall())` without
// an intermediate nil check.
func WithExit(code int, err error) error {
	if err == nil {
		return nil
	}
	return &ExitCodeError{Code: code, Err: err}
}
