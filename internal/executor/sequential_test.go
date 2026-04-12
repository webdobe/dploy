package executor

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/webdobe/dploy/internal/operation"
	"github.com/webdobe/dploy/internal/planner"
)

// Test helpers.

// localTarget builds a TargetPlan that runs against the real LocalRunner.
// commands are executed by /bin/sh -c, so `true`, `false`, and `echo ...`
// are reliable across macOS and Linux.
func localTarget(name string, commands ...string) planner.TargetPlan {
	steps := make([]planner.Step, len(commands))
	for i, c := range commands {
		steps[i] = planner.Step{Index: i, Command: c}
	}
	return planner.TargetPlan{
		Name:  name,
		Type:  "local",
		Path:  ".",
		Steps: steps,
	}
}

// bogusTarget builds a TargetPlan with an unknown type, which forces
// RunnerFor to return an UnsupportedTargetError. Used to exercise the
// failed_resolution path in Sequential.
func bogusTarget(name string, commands ...string) planner.TargetPlan {
	tp := localTarget(name, commands...)
	tp.Type = "bogus"
	return tp
}

func newPlan(tt ...planner.TargetPlan) *planner.Plan {
	return &planner.Plan{
		Operation:   operation.TypeDeploy,
		Environment: "test",
		Targets:     tt,
	}
}

func TestSequential_SingleTargetAllSucceed(t *testing.T) {
	plan := newPlan(localTarget("dev", "true", "echo hi"))
	result, err := NewSequential(nil, nil).Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != operation.StatusSuccess {
		t.Errorf("Status = %v; want success", result.Status)
	}
	if len(result.Steps) != 2 {
		t.Errorf("Steps = %d; want 2", len(result.Steps))
	}
	for _, s := range result.Steps {
		if s.Status != operation.StatusSuccess {
			t.Errorf("step %q: Status = %v; want success", s.Command, s.Status)
		}
		if s.Duration <= 0 {
			t.Errorf("step %q: Duration = %v; want positive", s.Command, s.Duration)
		}
	}
}

func TestSequential_SingleTargetStepFailureStopsRemainingSteps(t *testing.T) {
	plan := newPlan(localTarget("dev", "true", "false", "echo never"))
	result, err := NewSequential(nil, nil).Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != operation.StatusFailedExecution {
		t.Errorf("Status = %v; want failed_execution", result.Status)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("Steps = %d; want 2 (third step must not run)", len(result.Steps))
	}
	if result.Steps[1].ExitCode != 1 {
		t.Errorf("failed step ExitCode = %d; want 1", result.Steps[1].ExitCode)
	}
	if result.Steps[1].Status != operation.StatusFailedExecution {
		t.Errorf("failed step Status = %v; want failed_execution", result.Steps[1].Status)
	}
}

func TestSequential_MultiTargetAllSucceed(t *testing.T) {
	plan := newPlan(
		localTarget("a", "true"),
		localTarget("b", "true"),
		localTarget("c", "true"),
	)
	result, _ := NewSequential(nil, nil).Execute(context.Background(), plan)
	if result.Status != operation.StatusSuccess {
		t.Errorf("Status = %v; want success", result.Status)
	}
	if len(result.Steps) != 3 {
		t.Errorf("Steps = %d; want 3", len(result.Steps))
	}
}

func TestSequential_MultiTargetFirstTargetFailsIsFailedExecution(t *testing.T) {
	// No earlier target completed, so the result is failed_execution, not partial_failure.
	plan := newPlan(
		localTarget("a", "false"),
		localTarget("b", "true"),
	)
	result, _ := NewSequential(nil, nil).Execute(context.Background(), plan)
	if result.Status != operation.StatusFailedExecution {
		t.Errorf("Status = %v; want failed_execution", result.Status)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("Steps = %d; want 1 (target b must not run after a fails)", len(result.Steps))
	}
	if result.Steps[0].Target != "a" {
		t.Errorf("recorded step target = %q; want a", result.Steps[0].Target)
	}
}

func TestSequential_MultiTargetLaterFailureIsPartialFailure(t *testing.T) {
	plan := newPlan(
		localTarget("a", "true", "echo a-done"),
		localTarget("b", "false"),
		localTarget("c", "true"), // never reached
	)
	result, _ := NewSequential(nil, nil).Execute(context.Background(), plan)
	if result.Status != operation.StatusPartialFailure {
		t.Errorf("Status = %v; want partial_failure", result.Status)
	}
	// a: 2 steps, b: 1 step (the fail), c: 0. Total: 3.
	if len(result.Steps) != 3 {
		t.Errorf("Steps = %d; want 3", len(result.Steps))
	}
	// No step for c must exist.
	for _, s := range result.Steps {
		if s.Target == "c" {
			t.Errorf("target c should not have run, but got step: %+v", s)
		}
	}
}

func TestSequential_UnsupportedTargetFirstIsFailedExecution(t *testing.T) {
	// Bogus target first → resolution failure for it, subsequent target never runs.
	plan := newPlan(
		bogusTarget("weird", "true"),
		localTarget("ok", "true"),
	)
	result, _ := NewSequential(nil, nil).Execute(context.Background(), plan)
	if result.Status != operation.StatusFailedExecution {
		t.Errorf("Status = %v; want failed_execution", result.Status)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("Steps = %d; want 1 (resolution failure only)", len(result.Steps))
	}
	if result.Steps[0].Status != operation.StatusFailedResolution {
		t.Errorf("Status = %v; want failed_resolution", result.Steps[0].Status)
	}
	if result.Steps[0].Target != "weird" {
		t.Errorf("Target = %q; want weird", result.Steps[0].Target)
	}
}

func TestSequential_UnsupportedTargetAfterSuccessIsPartialFailure(t *testing.T) {
	// Target "ok" completes fully before "weird" fails resolution.
	plan := newPlan(
		localTarget("ok", "true"),
		bogusTarget("weird", "true"),
	)
	result, _ := NewSequential(nil, nil).Execute(context.Background(), plan)
	if result.Status != operation.StatusPartialFailure {
		t.Errorf("Status = %v; want partial_failure", result.Status)
	}
}

func TestSequential_StreamReceivesLiveStepOutput(t *testing.T) {
	var buf bytes.Buffer
	plan := newPlan(localTarget("dev", "echo streamed-marker"))
	_, err := NewSequential(&buf, nil).Execute(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "streamed-marker") {
		t.Errorf("stream output = %q; want to contain 'streamed-marker'", buf.String())
	}
}

func TestSequential_AnnouncerInvokedOncePerStepInOrder(t *testing.T) {
	var got []string
	announce := func(target string, index, total int, command string) {
		got = append(got, fmt.Sprintf("%s:%d/%d:%s", target, index, total, command))
	}
	plan := newPlan(
		localTarget("a", "true", "echo one"),
		localTarget("b", "true"),
	)
	NewSequential(nil, announce).Execute(context.Background(), plan)

	want := []string{"a:1/2:true", "a:2/2:echo one", "b:1/1:true"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("announcer calls = %v; want %v", got, want)
	}
}

func TestSequential_EmptyPlanIsFailedExecution(t *testing.T) {
	// The planner rejects empty plans before this point, but the executor
	// should still fail closed rather than report success on no work.
	plan := &planner.Plan{Operation: operation.TypeDeploy, Environment: "test"}
	result, err := NewSequential(nil, nil).Execute(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != operation.StatusFailedExecution {
		t.Errorf("Status = %v; want failed_execution for an empty plan", result.Status)
	}
}

func TestSequential_TargetEnvIsVisibleToSteps(t *testing.T) {
	// Exercises the LocalRunner env-merge path used by sync workflows.
	// When target.Env is set, those vars must reach the step's shell
	// alongside the inherited os.Environ (scripts still need PATH etc.).
	var buf bytes.Buffer
	plan := newPlan(planner.TargetPlan{
		Name: "local", Type: "local", Path: ".",
		Env: map[string]string{
			"DPLOY_SYNC_TEST_MARKER": "hello-from-env",
		},
		Steps: []planner.Step{
			{Index: 0, Command: `echo "marker=$DPLOY_SYNC_TEST_MARKER"`},
		},
	})
	result, err := NewSequential(&buf, nil).Execute(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != operation.StatusSuccess {
		t.Fatalf("Status = %v; want success", result.Status)
	}
	if !strings.Contains(result.Steps[0].Output, "marker=hello-from-env") {
		t.Errorf("step output = %q; want to contain 'marker=hello-from-env'", result.Steps[0].Output)
	}
}

func TestSequential_StepIndexPreservedFromPlan(t *testing.T) {
	// The step's Index in the config may not be contiguous after
	// filtering, but the executor must echo it back unchanged on the
	// recorded StepResult (state + logs rely on this).
	plan := newPlan(planner.TargetPlan{
		Name: "dev", Type: "local", Path: ".",
		Steps: []planner.Step{
			{Index: 3, Command: "true"},
			{Index: 7, Command: "true"},
		},
	})
	result, _ := NewSequential(nil, nil).Execute(context.Background(), plan)
	if len(result.Steps) != 2 {
		t.Fatalf("Steps = %d; want 2", len(result.Steps))
	}
	if result.Steps[0].Index != 3 || result.Steps[1].Index != 7 {
		t.Errorf("indices = [%d, %d]; want [3, 7]", result.Steps[0].Index, result.Steps[1].Index)
	}
}
