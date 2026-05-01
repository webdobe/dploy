package cli

import (
	"fmt"
	"strings"

	"github.com/webdobe/dploy/internal/operation"
)

// formatFailedStep returns a human-readable summary of the first
// non-success step in result.Steps, suitable for inclusion in the
// error message when an operation ends in StatusFailedExecution or
// StatusPartialFailure.
//
// Step output is normally streamed to stdout as it runs, but two cases
// produce no streamed output:
//
//  1. connect-time failures (e.g. SSH auth) — they fail before any
//     command runs, so there's nothing to stream; the diagnosis lives
//     only on StepResult.Error.
//  2. --quiet runs — streaming is suppressed but Output is still
//     captured.
//
// In both cases "see logs for the failing step" sends the user
// hunting; surfacing the step's command, exit code, error, and (when
// it wasn't already streamed) captured output makes the failure
// self-explanatory.
func formatFailedStep(result *operation.Result) string {
	if result == nil {
		return ""
	}
	for _, s := range result.Steps {
		if s.Status == operation.StatusSuccess {
			continue
		}
		var b strings.Builder
		// Step indices are zero-based internally; humans count from 1.
		fmt.Fprintf(&b, "step %d", s.Index+1)
		if s.Target != "" {
			fmt.Fprintf(&b, " on %s", s.Target)
		}
		if s.Command != "" {
			fmt.Fprintf(&b, ": %s", singleLine(s.Command))
		}
		// Skip the synthetic -1 we use when no real exit code is known
		// (process didn't start, ssh connect failed, etc.). 0 is also
		// uninteresting — the failure was non-exit-code-shaped.
		if s.ExitCode > 0 {
			fmt.Fprintf(&b, " (exit %d)", s.ExitCode)
		}
		if s.Error != "" {
			fmt.Fprintf(&b, "\n  %s", strings.TrimRight(s.Error, "\n"))
		}
		// Only attach captured output when streaming was suppressed.
		// Otherwise the user already saw it inline and we'd duplicate.
		if quiet && s.Output != "" {
			fmt.Fprintf(&b, "\n  output:\n%s", indentLines(strings.TrimRight(s.Output, "\n"), "    "))
		}
		return b.String()
	}
	return ""
}

// failedStepSuffix is the convenience wrapper call sites use when
// composing their own error message: it returns either a leading ":"
// + the formatted step, or — when no failing step is recorded — the
// classic "see logs for the failing step" hint.
func failedStepSuffix(result *operation.Result) string {
	if d := formatFailedStep(result); d != "" {
		return ":\n  " + strings.ReplaceAll(d, "\n", "\n  ")
	}
	return ": see logs for the failing step"
}

// singleLine collapses multi-line commands to a single line so the
// "step N: ..." header stays readable. Multi-line commands are common
// in dploy.yml (heredocs, &&-chains across lines).
func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ⏎ ")
	return strings.TrimSpace(s)
}

func indentLines(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
