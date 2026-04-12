// Package environment resolves a named environment from config into a
// concrete set of targets, roles, and strategy ready for planning.
//
// Resolution is deterministic and side-effect free. It does not connect
// to any hosts, evaluate policy, or execute anything.
package environment

import (
	"fmt"
	"sort"

	"github.com/webdobe/dploy/internal/config"
)

// Resolved is an environment with its targets expanded and ordered.
type Resolved struct {
	Name     string
	Class    string
	Strategy config.Strategy
	Targets  []Target
}

// Target is a single resolved target within an environment.
type Target struct {
	Name  string
	Type  string
	Host  string
	Path  string
	Roles []string
}

// Resolve looks up env by name in cfg and returns its resolved form.
func Resolve(cfg *config.Config, name string) (*Resolved, error) {
	env, ok := cfg.Environments[name]
	if !ok {
		return nil, fmt.Errorf("environment %q not found in config", name)
	}

	r := &Resolved{
		Name:     name,
		Class:    env.Class,
		Strategy: env.Strategy,
	}
	if r.Strategy == "" {
		r.Strategy = config.StrategySequential
	}

	for tname, t := range env.Targets {
		r.Targets = append(r.Targets, Target{
			Name:  tname,
			Type:  t.Type,
			Host:  t.Host,
			Path:  t.Path,
			Roles: append([]string(nil), t.Roles...),
		})
	}

	// Deterministic target order for reproducible plans.
	sort.Slice(r.Targets, func(i, j int) bool {
		return r.Targets[i].Name < r.Targets[j].Name
	})

	return r, nil
}

// FilterByRole returns only targets that include the given role.
func FilterByRole(targets []Target, role string) []Target {
	var out []Target
	for _, t := range targets {
		for _, tr := range t.Roles {
			if tr == role {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

// FilterByName returns only targets whose name is in names.
func FilterByName(targets []Target, names []string) []Target {
	want := make(map[string]struct{}, len(names))
	for _, n := range names {
		want[n] = struct{}{}
	}
	var out []Target
	for _, t := range targets {
		if _, ok := want[t.Name]; ok {
			out = append(out, t)
		}
	}
	return out
}
