// Package policy loads and evaluates the trusted policy file.
//
// Trusted policy is separate from repo config by design (see TRUST_MODEL.md).
// Repo config describes what the project wants to do. Trusted policy
// describes what is allowed. Policy can restrict or require additional
// conditions; repo config can never override policy restrictions.
package policy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/webdobe/dploy/internal/operation"
)

// Policy is the set of rules loaded from a trusted policy source.
type Policy struct {
	Source string `json:"source,omitempty" yaml:"-"`
	Rules  []Rule `json:"rules,omitempty" yaml:"rules"`
}

// Rule is a single allow/deny entry with optional scope predicates.
//
// A rule matches a request when every non-empty field matches. The Action
// is then applied. Deny takes precedence over allow if multiple rules match.
type Rule struct {
	Operation   string   `yaml:"operation,omitempty"`
	SourceEnv   string   `yaml:"source,omitempty"`
	TargetEnv   string   `yaml:"target,omitempty"`
	SourceClass string   `yaml:"source_class,omitempty"`
	TargetClass string   `yaml:"target_class,omitempty"`
	Resources   []string `yaml:"resources,omitempty"`
	Action      Action   `yaml:"action"`
	Require     []string `yaml:"require,omitempty"`
	Reason      string   `yaml:"reason,omitempty"`
}

// Action is the disposition of a matching rule.
type Action string

const (
	ActionAllow Action = "allow"
	ActionDeny  Action = "deny"
)

// Decision is the outcome of evaluating a policy against a request.
//
// Requirements is every distinct require: item from matched rules.
// Unmet is the subset of Requirements that the request did not list
// under operation.Request.Satisfied — i.e. what the caller still has
// to acknowledge (typically via a CLI flag like --confirm). Callers
// should fail closed on Unmet, not on Requirements.
type Decision struct {
	Allowed      bool
	MatchedRule  *Rule
	Requirements []string
	Unmet        []string
	Reason       string
}

// Load reads a policy file from disk. A missing file returns an empty
// policy with nil error — the caller can decide whether that's acceptable
// for the environment class involved.
func Load(path string) (*Policy, error) {
	if path == "" {
		return &Policy{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Policy{Source: path}, nil
		}
		return nil, fmt.Errorf("read policy %s: %w", path, err)
	}

	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse policy %s: %w", path, err)
	}
	p.Source = path
	return &p, nil
}

// Evaluate returns a Decision for req against p. If no rule matches,
// the default disposition is Allow (policy is additive by default).
// Deny rules win over Allow rules when both match.
//
// Requirements is deduplicated and order-preserving across all matching
// rules. Unmet is Requirements minus req.Satisfied.
func (p *Policy) Evaluate(req operation.Request) Decision {
	d := Decision{Allowed: true}

	seenReq := map[string]bool{}
	for i := range p.Rules {
		r := &p.Rules[i]
		if !r.matches(req) {
			continue
		}
		d.MatchedRule = r
		d.Reason = r.Reason
		for _, item := range r.Require {
			if !seenReq[item] {
				seenReq[item] = true
				d.Requirements = append(d.Requirements, item)
			}
		}
		if r.Action == ActionDeny {
			d.Allowed = false
			return d
		}
	}

	// Compute unmet requirements against what the caller has acknowledged.
	if len(d.Requirements) > 0 {
		satisfied := map[string]bool{}
		for _, s := range req.Satisfied {
			satisfied[s] = true
		}
		for _, item := range d.Requirements {
			if !satisfied[item] {
				d.Unmet = append(d.Unmet, item)
			}
		}
	}

	return d
}

func (r *Rule) matches(req operation.Request) bool {
	if r.Operation != "" && r.Operation != string(req.Type) {
		return false
	}
	if r.SourceEnv != "" && r.SourceEnv != req.SourceEnv {
		return false
	}
	// TargetEnv matches the explicit TargetEnv (sync) or the single-env
	// Environment field (deploy-style). Either form is valid shorthand.
	if r.TargetEnv != "" && r.TargetEnv != req.TargetEnv && r.TargetEnv != req.Environment {
		return false
	}
	if r.SourceClass != "" && r.SourceClass != req.SourceClass {
		return false
	}
	// Same shorthand for class: TargetClass matches TargetClass (sync)
	// or the single-env Class field (deploy-style).
	if r.TargetClass != "" && r.TargetClass != req.TargetClass && r.TargetClass != req.Class {
		return false
	}
	if len(r.Resources) > 0 {
		for _, want := range r.Resources {
			found := false
			for _, got := range req.Resources {
				if got == want {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}
