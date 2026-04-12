package planner

import (
	"reflect"
	"strings"
	"testing"

	"github.com/webdobe/dploy/internal/config"
	"github.com/webdobe/dploy/internal/environment"
	"github.com/webdobe/dploy/internal/operation"
)

// helper: extract command strings from a slice of Step in order.
func commands(steps []Step) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Command
	}
	return out
}

// helper: locate a target's plan by name, or fail the test.
func targetPlan(t *testing.T, p *Plan, name string) TargetPlan {
	t.Helper()
	for _, tp := range p.Targets {
		if tp.Name == name {
			return tp
		}
	}
	t.Fatalf("target %q not present in plan (targets: %v)", name, planTargetNames(p))
	return TargetPlan{}
}

func planTargetNames(p *Plan) []string {
	out := make([]string, 0, len(p.Targets))
	for _, tp := range p.Targets {
		out = append(out, tp.Name)
	}
	return out
}

// helper: minimal resolved env built from cfg by calling the real resolver.
func resolve(t *testing.T, cfg *config.Config, name string) *environment.Resolved {
	t.Helper()
	r, err := environment.Resolve(cfg, name)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", name, err)
	}
	return r
}

func TestBuildDeploy_AllTargetsGetAllUnfilteredSteps(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"staging": {
				Class: "staging",
				Targets: map[string]config.Target{
					"a": {Type: "local", Path: "/srv"},
					"b": {Type: "local", Path: "/srv"},
				},
				Deploy: []config.Step{
					{Run: "git pull"},
					{Run: "restart"},
				},
			},
		},
	}
	plan, err := BuildDeploy(operation.Request{Type: operation.TypeDeploy, Environment: "staging"}, cfg, resolve(t, cfg, "staging"))
	if err != nil {
		t.Fatalf("BuildDeploy: %v", err)
	}

	if plan.Operation != operation.TypeDeploy {
		t.Errorf("Operation = %v; want deploy", plan.Operation)
	}
	if plan.Environment != "staging" {
		t.Errorf("Environment = %q; want staging", plan.Environment)
	}
	if plan.Class != "staging" {
		t.Errorf("Class = %q; want staging", plan.Class)
	}
	if len(plan.Targets) != 2 {
		t.Fatalf("Targets = %d; want 2", len(plan.Targets))
	}
	for _, tp := range plan.Targets {
		got := commands(tp.Steps)
		want := []string{"git pull", "restart"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("target %s: commands = %v; want %v", tp.Name, got, want)
		}
	}
}

func TestBuildDeploy_OnRoleFiltersSteps(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"staging": {
				Targets: map[string]config.Target{
					"web1":    {Type: "local", Path: "/srv", Roles: []string{"web"}},
					"worker1": {Type: "local", Path: "/srv", Roles: []string{"worker"}},
				},
				Deploy: []config.Step{
					{Run: "common"},
					{Run: "web-only", OnRole: "web"},
					{Run: "worker-only", OnRole: "worker"},
				},
			},
		},
	}
	plan, err := BuildDeploy(operation.Request{Type: operation.TypeDeploy, Environment: "staging"}, cfg, resolve(t, cfg, "staging"))
	if err != nil {
		t.Fatalf("BuildDeploy: %v", err)
	}

	got := map[string][]string{}
	for _, tp := range plan.Targets {
		got[tp.Name] = commands(tp.Steps)
	}
	want := map[string][]string{
		"web1":    {"common", "web-only"},
		"worker1": {"common", "worker-only"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("per-target commands = %v; want %v", got, want)
	}
}

func TestBuildDeploy_OnFiltersStepsByTargetName(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"s": {
				Targets: map[string]config.Target{
					"a": {Type: "local", Path: "/p"},
					"b": {Type: "local", Path: "/p"},
					"c": {Type: "local", Path: "/p"},
				},
				Deploy: []config.Step{
					{Run: "all"},
					{Run: "a-and-c-only", On: []string{"a", "c"}},
				},
			},
		},
	}
	plan, err := BuildDeploy(operation.Request{Type: operation.TypeDeploy, Environment: "s"}, cfg, resolve(t, cfg, "s"))
	if err != nil {
		t.Fatal(err)
	}

	if got := commands(targetPlan(t, plan, "a").Steps); !reflect.DeepEqual(got, []string{"all", "a-and-c-only"}) {
		t.Errorf("a: %v", got)
	}
	if got := commands(targetPlan(t, plan, "b").Steps); !reflect.DeepEqual(got, []string{"all"}) {
		t.Errorf("b: %v", got)
	}
	if got := commands(targetPlan(t, plan, "c").Steps); !reflect.DeepEqual(got, []string{"all", "a-and-c-only"}) {
		t.Errorf("c: %v", got)
	}
}

func TestBuildDeploy_OnAndOnRoleCombineAsAnd(t *testing.T) {
	// Step requires BOTH role=web AND name in {a, c}.
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"s": {
				Targets: map[string]config.Target{
					"a": {Type: "local", Path: "/p", Roles: []string{"web"}},
					"b": {Type: "local", Path: "/p", Roles: []string{"worker"}}, // fails role
					"c": {Type: "local", Path: "/p", Roles: []string{"web"}},
					"d": {Type: "local", Path: "/p", Roles: []string{"web"}}, // fails name filter
				},
				Deploy: []config.Step{
					{Run: "picky", On: []string{"a", "c"}, OnRole: "web"},
				},
			},
		},
	}
	plan, err := BuildDeploy(operation.Request{Type: operation.TypeDeploy, Environment: "s"}, cfg, resolve(t, cfg, "s"))
	if err != nil {
		t.Fatal(err)
	}
	// Only a and c should appear in the plan (others have no matching steps and are omitted).
	if names := planTargetNames(plan); !reflect.DeepEqual(sortedCopy(names), []string{"a", "c"}) {
		t.Errorf("target names = %v; want [a c]", names)
	}
}

func TestBuildDeploy_StepIndexReflectsOriginalConfigPosition(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"s": {
				Targets: map[string]config.Target{
					"web": {Type: "local", Path: "/p", Roles: []string{"web"}},
				},
				Deploy: []config.Step{
					{Run: "worker-only", OnRole: "worker"}, // filtered out for web
					{Run: "common"},
					{Run: "worker-only-2", OnRole: "worker"}, // filtered out
					{Run: "web-only", OnRole: "web"},
				},
			},
		},
	}
	plan, err := BuildDeploy(operation.Request{Type: operation.TypeDeploy, Environment: "s"}, cfg, resolve(t, cfg, "s"))
	if err != nil {
		t.Fatal(err)
	}
	tp := targetPlan(t, plan, "web")
	wantIdx := []int{1, 3}
	gotIdx := make([]int, len(tp.Steps))
	for i, s := range tp.Steps {
		gotIdx[i] = s.Index
	}
	if !reflect.DeepEqual(gotIdx, wantIdx) {
		t.Errorf("indices = %v; want %v (indices must match config step positions, not surviving order)", gotIdx, wantIdx)
	}
}

func TestBuildDeploy_TargetWithNoMatchingStepsIsOmitted(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"s": {
				Targets: map[string]config.Target{
					"web":    {Type: "local", Path: "/p", Roles: []string{"web"}},
					"worker": {Type: "local", Path: "/p", Roles: []string{"worker"}},
				},
				Deploy: []config.Step{
					{Run: "web-only", OnRole: "web"},
				},
			},
		},
	}
	plan, err := BuildDeploy(operation.Request{Type: operation.TypeDeploy, Environment: "s"}, cfg, resolve(t, cfg, "s"))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Targets) != 1 || plan.Targets[0].Name != "web" {
		t.Errorf("plan targets = %v; want only [web]", planTargetNames(plan))
	}
}

func TestBuildDeploy_NoStepsMatchAnyTargetReturnsError(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"s": {
				Targets: map[string]config.Target{
					"web": {Type: "local", Path: "/p", Roles: []string{"web"}},
				},
				Deploy: []config.Step{
					{Run: "worker-only", OnRole: "worker"},
				},
			},
		},
	}
	if _, err := BuildDeploy(operation.Request{Type: operation.TypeDeploy, Environment: "s"}, cfg, resolve(t, cfg, "s")); err == nil {
		t.Fatal("expected error for plan with no executable steps, got nil")
	}
}

func TestBuildDeploy_RequestTargetsFilter(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"s": {
				Targets: map[string]config.Target{
					"a": {Type: "local", Path: "/p"},
					"b": {Type: "local", Path: "/p"},
					"c": {Type: "local", Path: "/p"},
				},
				Deploy: []config.Step{{Run: "work"}},
			},
		},
	}
	req := operation.Request{Type: operation.TypeDeploy, Environment: "s", Targets: []string{"a", "c"}}
	plan, err := BuildDeploy(req, cfg, resolve(t, cfg, "s"))
	if err != nil {
		t.Fatal(err)
	}
	if names := planTargetNames(plan); !reflect.DeepEqual(sortedCopy(names), []string{"a", "c"}) {
		t.Errorf("plan targets = %v; want [a c]", names)
	}
}

func TestBuildDeploy_RequestTargetsWithNoMatchReturnsError(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"s": {
				Targets: map[string]config.Target{"a": {Type: "local", Path: "/p"}},
				Deploy:  []config.Step{{Run: "work"}},
			},
		},
	}
	req := operation.Request{Type: operation.TypeDeploy, Environment: "s", Targets: []string{"nonexistent"}}
	if _, err := BuildDeploy(req, cfg, resolve(t, cfg, "s")); err == nil {
		t.Fatal("expected error when requested target names don't match any resolved target")
	}
}

func TestBuildDeploy_ArtifactPassThrough(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"s": {
				Targets: map[string]config.Target{"a": {Type: "local", Path: "/p"}},
				Deploy:  []config.Step{{Run: "work"}},
			},
		},
	}
	req := operation.Request{Type: operation.TypeDeploy, Environment: "s", Artifact: "v1.2.3"}
	plan, err := BuildDeploy(req, cfg, resolve(t, cfg, "s"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Artifact != "v1.2.3" {
		t.Errorf("Artifact = %q; want v1.2.3", plan.Artifact)
	}
}

func TestBuildRollback_UsesRollbackStepsNotDeploy(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"s": {
				Targets: map[string]config.Target{"a": {Type: "local", Path: "/p"}},
				Deploy:  []config.Step{{Run: "deploy-step"}},
				Rollback: []config.Step{
					{Run: "restore-db"},
					{Run: "restart"},
				},
			},
		},
	}
	plan, err := BuildRollback(operation.Request{Type: operation.TypeRollback, Environment: "s"}, cfg, resolve(t, cfg, "s"))
	if err != nil {
		t.Fatalf("BuildRollback: %v", err)
	}
	if plan.Operation != operation.TypeRollback {
		t.Errorf("Operation = %v; want rollback", plan.Operation)
	}
	got := commands(targetPlan(t, plan, "a").Steps)
	want := []string{"restore-db", "restart"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commands = %v; want %v (must come from rollback block, not deploy)", got, want)
	}
}

func TestBuildRollback_NoRollbackDefinedReturnsClearError(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"s": {
				Targets: map[string]config.Target{"a": {Type: "local", Path: "/p"}},
				Deploy:  []config.Step{{Run: "deploy-step"}},
				// no Rollback block
			},
		},
	}
	_, err := BuildRollback(operation.Request{Type: operation.TypeRollback, Environment: "s"}, cfg, resolve(t, cfg, "s"))
	if err == nil {
		t.Fatal("expected error when environment has no rollback steps defined")
	}
	// CLI maps this error to ExitRollbackUnavail; the message is what
	// users see, so lock in that it's specific rather than the generic
	// "plan has no executable steps" message used elsewhere.
	if !strings.Contains(err.Error(), "no rollback steps defined") {
		t.Errorf("error = %q; want to contain 'no rollback steps defined'", err)
	}
}

func TestBuildRollback_RespectsOnRoleFilter(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"s": {
				Targets: map[string]config.Target{
					"web":    {Type: "local", Path: "/p", Roles: []string{"web"}},
					"worker": {Type: "local", Path: "/p", Roles: []string{"worker"}},
				},
				Rollback: []config.Step{
					{Run: "web-restore", OnRole: "web"},
					{Run: "worker-restore", OnRole: "worker"},
				},
			},
		},
	}
	plan, err := BuildRollback(operation.Request{Type: operation.TypeRollback, Environment: "s"}, cfg, resolve(t, cfg, "s"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, tp := range plan.Targets {
		got[tp.Name] = commands(tp.Steps)
	}
	want := map[string][]string{
		"web":    {"web-restore"},
		"worker": {"worker-restore"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("per-target commands = %v; want %v", got, want)
	}
}

// --- BuildSync tests ---

func TestBuildSync_SingleResourceExpandsWorkflow(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"production": {
				Class:   "production",
				Targets: map[string]config.Target{"web": {Type: "local", Path: "/p"}},
				Data: map[string][]string{
					"database": {"./dump.sh", "./sanitize.sh", "./restore.sh"},
				},
			},
			"local": {
				Class:   "development",
				Targets: map[string]config.Target{"dev": {Type: "local", Path: "."}},
			},
		},
	}
	source := resolve(t, cfg, "production")
	target := resolve(t, cfg, "local")

	req := operation.Request{
		Type:        operation.TypeSync,
		SourceEnv:   "production",
		SourceClass: "production",
		TargetEnv:   "local",
		TargetClass: "development",
		Resources:   []string{"database"},
	}
	plan, err := BuildSync(req, cfg, source, target)
	if err != nil {
		t.Fatalf("BuildSync: %v", err)
	}

	if plan.Operation != operation.TypeSync {
		t.Errorf("Operation = %v; want sync", plan.Operation)
	}
	// Plan is keyed on target env so `dploy status local` surfaces the sync.
	if plan.Environment != "local" {
		t.Errorf("Environment = %q; want local (state keys on target)", plan.Environment)
	}
	if plan.Class != "development" {
		t.Errorf("Class = %q; want development (target class)", plan.Class)
	}
	if len(plan.Targets) != 1 {
		t.Fatalf("Targets = %d; want 1 (local exec context)", len(plan.Targets))
	}
	tp := plan.Targets[0]
	if tp.Name != "local" || tp.Type != "local" || tp.Path != "." {
		t.Errorf("target = %+v; want {local, local, .}", tp)
	}
	got := commands(tp.Steps)
	want := []string{"./dump.sh", "./sanitize.sh", "./restore.sh"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commands = %v; want %v", got, want)
	}
	// Env vars that scripts rely on.
	wantEnv := map[string]string{
		"DPLOY_SOURCE":       "production",
		"DPLOY_TARGET":       "local",
		"DPLOY_SOURCE_CLASS": "production",
		"DPLOY_TARGET_CLASS": "development",
		"DPLOY_RESOURCES":    "database",
	}
	if !reflect.DeepEqual(tp.Env, wantEnv) {
		t.Errorf("env = %v; want %v", tp.Env, wantEnv)
	}
}

func TestBuildSync_MultipleResourcesConcatenateInOrder(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"production": {
				Targets: map[string]config.Target{"web": {Type: "local", Path: "/p"}},
				Data: map[string][]string{
					"database": {"db-1", "db-2"},
					"files":    {"files-1"},
				},
			},
			"local": {
				Targets: map[string]config.Target{"dev": {Type: "local", Path: "."}},
			},
		},
	}
	req := operation.Request{
		Type: operation.TypeSync, SourceEnv: "production", TargetEnv: "local",
		Resources: []string{"database", "files"},
	}
	plan, err := BuildSync(req, cfg, resolve(t, cfg, "production"), resolve(t, cfg, "local"))
	if err != nil {
		t.Fatal(err)
	}

	// Resource order is preserved, and within a resource the workflow's step order is preserved.
	got := commands(plan.Targets[0].Steps)
	want := []string{"db-1", "db-2", "files-1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commands = %v; want %v", got, want)
	}
	// Step indices are global across the plan, not per-resource.
	for i, s := range plan.Targets[0].Steps {
		if s.Index != i {
			t.Errorf("step %d Index = %d; want %d", i, s.Index, i)
		}
	}
	if plan.Targets[0].Env["DPLOY_RESOURCES"] != "database,files" {
		t.Errorf("DPLOY_RESOURCES = %q; want database,files", plan.Targets[0].Env["DPLOY_RESOURCES"])
	}
}

func TestBuildSync_UnknownResourceListsAvailable(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"production": {
				Targets: map[string]config.Target{"web": {Type: "local", Path: "/p"}},
				Data: map[string][]string{
					"database": {"./dump.sh"},
					"files":    {"./files.sh"},
				},
			},
			"local": {
				Targets: map[string]config.Target{"dev": {Type: "local", Path: "."}},
			},
		},
	}
	req := operation.Request{
		Type: operation.TypeSync, SourceEnv: "production", TargetEnv: "local",
		Resources: []string{"media"},
	}
	_, err := BuildSync(req, cfg, resolve(t, cfg, "production"), resolve(t, cfg, "local"))
	if err == nil {
		t.Fatal("expected error for unknown resource")
	}
	// The error should name the missing resource AND list what IS available,
	// alphabetically, so users can correct a typo without reading docs.
	msg := err.Error()
	if !strings.Contains(msg, "media") {
		t.Errorf("error = %q; expected to mention 'media'", msg)
	}
	if !strings.Contains(msg, "database") || !strings.Contains(msg, "files") {
		t.Errorf("error = %q; expected to list available workflows", msg)
	}
}

func TestBuildSync_NoDataBlockReturnsClearError(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"production": {
				Targets: map[string]config.Target{"web": {Type: "local", Path: "/p"}},
				// no Data block
			},
			"local": {
				Targets: map[string]config.Target{"dev": {Type: "local", Path: "."}},
			},
		},
	}
	req := operation.Request{
		Type: operation.TypeSync, SourceEnv: "production", TargetEnv: "local",
		Resources: []string{"database"},
	}
	_, err := BuildSync(req, cfg, resolve(t, cfg, "production"), resolve(t, cfg, "local"))
	if err == nil {
		t.Fatal("expected error when source env has no data workflows")
	}
	if !strings.Contains(err.Error(), "no data: workflows defined") {
		t.Errorf("error = %q; want to mention 'no data: workflows defined'", err)
	}
}

func TestBuildSync_RequiresResources(t *testing.T) {
	cfg := &config.Config{
		App: "x",
		Environments: map[string]config.Environment{
			"production": {
				Targets: map[string]config.Target{"web": {Type: "local", Path: "/p"}},
				Data:    map[string][]string{"database": {"./dump.sh"}},
			},
			"local": {
				Targets: map[string]config.Target{"dev": {Type: "local", Path: "."}},
			},
		},
	}
	req := operation.Request{
		Type: operation.TypeSync, SourceEnv: "production", TargetEnv: "local",
		// no Resources
	}
	if _, err := BuildSync(req, cfg, resolve(t, cfg, "production"), resolve(t, cfg, "local")); err == nil {
		t.Fatal("expected error when Resources is empty")
	}
}

// sortedCopy returns a sorted copy of s without mutating the input.
// Used to make order-independent target-name comparisons predictable.
func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
