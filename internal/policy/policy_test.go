package policy

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/webdobe/dploy/internal/operation"
)

// sliceOrNil normalizes empty/nil slices so reflect.DeepEqual treats
// them as equal. Requirements/Unmet accumulate via append and can be
// nil when no rule matches; test cases use nil and []string{} interchangeably.
func sliceOrNil(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name        string
		policy      Policy
		req         operation.Request
		wantAllowed bool
		wantMatched bool
		wantReqs    []string
		wantUnmet   []string
		wantReason  string
	}{
		{
			name:        "empty policy allows",
			policy:      Policy{},
			req:         operation.Request{Type: operation.TypeDeploy, Environment: "production"},
			wantAllowed: true,
			wantMatched: false,
		},
		{
			name: "deny by operation",
			policy: Policy{Rules: []Rule{
				{Operation: "deploy", Action: ActionDeny, Reason: "freeze in effect"},
			}},
			req:         operation.Request{Type: operation.TypeDeploy, Environment: "staging"},
			wantAllowed: false,
			wantMatched: true,
			wantReason:  "freeze in effect",
		},
		{
			name: "deny by target env matches Environment for deploy",
			policy: Policy{Rules: []Rule{
				{TargetEnv: "production", Action: ActionDeny, Reason: "prod locked"},
			}},
			req:         operation.Request{Type: operation.TypeDeploy, Environment: "production"},
			wantAllowed: false,
			wantMatched: true,
		},
		{
			name: "non-matching target env lets request through",
			policy: Policy{Rules: []Rule{
				{TargetEnv: "production", Action: ActionDeny},
			}},
			req:         operation.Request{Type: operation.TypeDeploy, Environment: "staging"},
			wantAllowed: true,
			wantMatched: false,
		},
		{
			name: "deny by target class matches Class for deploy",
			policy: Policy{Rules: []Rule{
				{TargetClass: "production", Action: ActionDeny, Reason: "class production is locked"},
			}},
			req:         operation.Request{Type: operation.TypeDeploy, Environment: "prod-us", Class: "production"},
			wantAllowed: false,
			wantMatched: true,
		},
		{
			name: "non-matching target class lets request through",
			policy: Policy{Rules: []Rule{
				{TargetClass: "production", Action: ActionDeny},
			}},
			req:         operation.Request{Type: operation.TypeDeploy, Environment: "stg", Class: "staging"},
			wantAllowed: true,
		},
		{
			name: "deny wins over earlier allow",
			policy: Policy{Rules: []Rule{
				{Operation: "deploy", Action: ActionAllow},
				{TargetEnv: "production", Action: ActionDeny, Reason: "frozen"},
			}},
			req:         operation.Request{Type: operation.TypeDeploy, Environment: "production"},
			wantAllowed: false,
			wantMatched: true,
			wantReason:  "frozen",
		},
		{
			name: "allow with requirements surfaces the requirements as unmet",
			policy: Policy{Rules: []Rule{
				{TargetEnv: "production", Action: ActionAllow, Require: []string{"confirm"}},
			}},
			req:         operation.Request{Type: operation.TypeDeploy, Environment: "production"},
			wantAllowed: true,
			wantMatched: true,
			wantReqs:    []string{"confirm"},
			wantUnmet:   []string{"confirm"},
		},
		{
			name: "requirements acknowledged by Satisfied are not unmet",
			policy: Policy{Rules: []Rule{
				{TargetEnv: "production", Action: ActionAllow, Require: []string{"confirm"}},
			}},
			req: operation.Request{
				Type: operation.TypeDeploy, Environment: "production",
				Satisfied: []string{"confirm"},
			},
			wantAllowed: true,
			wantMatched: true,
			wantReqs:    []string{"confirm"},
			wantUnmet:   nil,
		},
		{
			name: "partial satisfaction leaves only the missing items unmet",
			policy: Policy{Rules: []Rule{
				{TargetEnv: "production", Action: ActionAllow, Require: []string{"confirm", "sanitization"}},
			}},
			req: operation.Request{
				Type: operation.TypeDeploy, Environment: "production",
				Satisfied: []string{"confirm"},
			},
			wantAllowed: true,
			wantMatched: true,
			wantReqs:    []string{"confirm", "sanitization"},
			wantUnmet:   []string{"sanitization"},
		},
		{
			name: "requirements from multiple matching rules are deduplicated",
			policy: Policy{Rules: []Rule{
				{Operation: "deploy", Action: ActionAllow, Require: []string{"confirm"}},
				{TargetEnv: "production", Action: ActionAllow, Require: []string{"confirm", "sanitization"}},
			}},
			req:         operation.Request{Type: operation.TypeDeploy, Environment: "production"},
			wantAllowed: true,
			wantMatched: true,
			wantReqs:    []string{"confirm", "sanitization"},
			wantUnmet:   []string{"confirm", "sanitization"},
		},
		{
			name: "extra Satisfied items that aren't required are benign",
			policy: Policy{Rules: []Rule{
				{TargetEnv: "production", Action: ActionAllow, Require: []string{"confirm"}},
			}},
			req: operation.Request{
				Type: operation.TypeDeploy, Environment: "production",
				Satisfied: []string{"confirm", "sanitization", "some-future-requirement"},
			},
			wantAllowed: true,
			wantMatched: true,
			wantReqs:    []string{"confirm"},
			wantUnmet:   nil,
		},
		{
			name: "resource rule does not match when resources differ",
			policy: Policy{Rules: []Rule{
				{Resources: []string{"database"}, Action: ActionDeny},
			}},
			req:         operation.Request{Type: operation.TypeSync, Resources: []string{"files"}},
			wantAllowed: true,
		},
		{
			name: "resource rule matches when requested resources include rule resources",
			policy: Policy{Rules: []Rule{
				{Resources: []string{"database"}, Action: ActionDeny, Reason: "db sync blocked"},
			}},
			req:         operation.Request{Type: operation.TypeSync, Resources: []string{"database", "files"}},
			wantAllowed: false,
			wantMatched: true,
			wantReason:  "db sync blocked",
		},
		{
			name: "sync source class matches SourceClass on request",
			policy: Policy{Rules: []Rule{
				{SourceClass: "production", Operation: "sync", Action: ActionDeny, Reason: "no sync from prod"},
			}},
			req: operation.Request{
				Type:        operation.TypeSync,
				SourceEnv:   "prod",
				SourceClass: "production",
				TargetEnv:   "local",
			},
			wantAllowed: false,
			wantMatched: true,
			wantReason:  "no sync from prod",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.policy.Evaluate(tc.req)

			if d.Allowed != tc.wantAllowed {
				t.Errorf("Allowed = %v; want %v", d.Allowed, tc.wantAllowed)
			}
			gotMatched := d.MatchedRule != nil
			if gotMatched != tc.wantMatched {
				t.Errorf("MatchedRule present = %v; want %v (matched=%+v)", gotMatched, tc.wantMatched, d.MatchedRule)
			}
			if tc.wantReason != "" && d.Reason != tc.wantReason {
				t.Errorf("Reason = %q; want %q", d.Reason, tc.wantReason)
			}
			if !reflect.DeepEqual(sliceOrNil(tc.wantReqs), sliceOrNil(d.Requirements)) {
				t.Errorf("Requirements = %v; want %v", d.Requirements, tc.wantReqs)
			}
			if !reflect.DeepEqual(sliceOrNil(tc.wantUnmet), sliceOrNil(d.Unmet)) {
				t.Errorf("Unmet = %v; want %v", d.Unmet, tc.wantUnmet)
			}
		})
	}
}

func TestLoad_MissingFileReturnsEmptyPolicy(t *testing.T) {
	// Per FAILURE_MODEL.md's resolution behavior and the CLI default
	// (/etc/dploy/policy.yml), a missing policy file is not an error —
	// an empty policy (allow all, no rules) is the safe baseline.
	p, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yml"))
	if err != nil {
		t.Fatalf("Load should not error on missing file, got: %v", err)
	}
	if p == nil {
		t.Fatal("Load returned nil policy")
	}
	if len(p.Rules) != 0 {
		t.Errorf("missing file should yield 0 rules, got %d", len(p.Rules))
	}

	d := p.Evaluate(operation.Request{Type: operation.TypeDeploy, Environment: "anything"})
	if !d.Allowed {
		t.Error("empty policy should allow")
	}
}

func TestLoad_RoundTripFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yml")
	yaml := `rules:
  - operation: deploy
    target: production
    target_class: production
    action: deny
    reason: production deploys are frozen
  - operation: sync
    source: production
    target: local
    resources: [database]
    action: allow
    require: [sanitization]
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Source != path {
		t.Errorf("Source = %q; want %q", p.Source, path)
	}
	if len(p.Rules) != 2 {
		t.Fatalf("Rules = %d; want 2", len(p.Rules))
	}

	// First rule should deny prod deploys.
	d := p.Evaluate(operation.Request{
		Type: operation.TypeDeploy, Environment: "production", Class: "production",
	})
	if d.Allowed {
		t.Error("expected deny for prod deploy")
	}
	if d.Reason != "production deploys are frozen" {
		t.Errorf("Reason = %q", d.Reason)
	}

	// Second rule should surface a sanitization requirement for a
	// prod→local database sync.
	d = p.Evaluate(operation.Request{
		Type:      operation.TypeSync,
		SourceEnv: "production",
		TargetEnv: "local",
		Resources: []string{"database"},
	})
	if !d.Allowed {
		t.Error("expected allow for sync rule")
	}
	if len(d.Requirements) != 1 || d.Requirements[0] != "sanitization" {
		t.Errorf("Requirements = %v; want [sanitization]", d.Requirements)
	}
}
