package cli

import (
	"strings"
	"testing"

	"github.com/webdobe/dploy/internal/operation"
)

func TestFormatFailedStep_NilOrAllSuccess(t *testing.T) {
	if got := formatFailedStep(nil); got != "" {
		t.Errorf("expected empty string for nil result, got %q", got)
	}
	r := &operation.Result{Steps: []operation.StepResult{
		{Index: 0, Command: "echo ok", Target: "vm", Status: operation.StatusSuccess},
	}}
	if got := formatFailedStep(r); got != "" {
		t.Errorf("expected empty string when all steps succeeded, got %q", got)
	}
}

func TestFormatFailedStep_ConnectFailureExposesError(t *testing.T) {
	// The case that motivated this — connect-time SSH auth failure
	// produces no Output, only Error, and a -1 exit code (no real
	// process ran). We must surface the Error.
	r := &operation.Result{Steps: []operation.StepResult{
		{
			Index:    0,
			Command:  "docker compose pull",
			Target:   "vm",
			Status:   operation.StatusFailedExecution,
			ExitCode: -1,
			Error:    "ssh connect: ssh: handshake failed: ssh: unable to authenticate",
		},
	}}
	got := formatFailedStep(r)
	if !strings.Contains(got, "step 1 on vm: docker compose pull") {
		t.Errorf("missing step header in output:\n%s", got)
	}
	if strings.Contains(got, "exit") {
		t.Errorf("exit code -1 should be hidden (no real process ran):\n%s", got)
	}
	if !strings.Contains(got, "ssh: unable to authenticate") {
		t.Errorf("missing Error in output:\n%s", got)
	}
}

func TestFormatFailedStep_ExitCodeShownForRealNonZero(t *testing.T) {
	r := &operation.Result{Steps: []operation.StepResult{
		{
			Index:    2,
			Command:  "false",
			Target:   "vm",
			Status:   operation.StatusFailedExecution,
			ExitCode: 1,
			Error:    "Process exited with status 1",
		},
	}}
	got := formatFailedStep(r)
	if !strings.Contains(got, "step 3") {
		t.Errorf("step number should be 1-indexed:\n%s", got)
	}
	if !strings.Contains(got, "(exit 1)") {
		t.Errorf("missing exit code:\n%s", got)
	}
}

func TestFormatFailedStep_OutputAttachedOnlyWhenQuiet(t *testing.T) {
	r := &operation.Result{Steps: []operation.StepResult{
		{
			Index:   0,
			Command: "do-thing",
			Target:  "vm",
			Status:  operation.StatusFailedExecution,
			Error:   "boom",
			Output:  "line one\nline two",
		},
	}}

	prevQuiet := quiet
	defer func() { quiet = prevQuiet }()

	quiet = false
	if got := formatFailedStep(r); strings.Contains(got, "line one") {
		t.Errorf("non-quiet runs should not duplicate streamed output:\n%s", got)
	}

	quiet = true
	got := formatFailedStep(r)
	if !strings.Contains(got, "line one") || !strings.Contains(got, "line two") {
		t.Errorf("quiet runs should include captured output:\n%s", got)
	}
}

func TestFormatFailedStep_PicksFirstFailure(t *testing.T) {
	r := &operation.Result{Steps: []operation.StepResult{
		{Index: 0, Command: "ok", Target: "vm", Status: operation.StatusSuccess},
		{Index: 1, Command: "first-fail", Target: "vm", Status: operation.StatusFailedExecution, Error: "first"},
		{Index: 2, Command: "second-fail", Target: "vm", Status: operation.StatusFailedExecution, Error: "second"},
	}}
	got := formatFailedStep(r)
	if !strings.Contains(got, "first-fail") || strings.Contains(got, "second-fail") {
		t.Errorf("expected first failure only:\n%s", got)
	}
}

func TestFormatFailedStep_CollapsesMultilineCommand(t *testing.T) {
	r := &operation.Result{Steps: []operation.StepResult{
		{
			Index:   0,
			Command: "set -e\nfoo bar\nbaz",
			Target:  "vm",
			Status:  operation.StatusFailedExecution,
			Error:   "x",
		},
	}}
	got := formatFailedStep(r)
	if strings.Count(got, "\n") > 1 {
		// Header line + one error line is fine; the command itself
		// should be on a single line.
		header := strings.SplitN(got, "\n", 2)[0]
		if strings.Contains(header, "\n") {
			t.Errorf("multi-line command should be collapsed in header:\n%s", got)
		}
	}
}

func TestFailedStepSuffix_FallbackWhenNoStepRecorded(t *testing.T) {
	r := &operation.Result{}
	got := failedStepSuffix(r)
	if got != ": see logs for the failing step" {
		t.Errorf("unexpected fallback suffix: %q", got)
	}
}
