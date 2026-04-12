// Package planner turns a validated request into an explicit execution plan.
//
// The plan is where ambiguity disappears: resolved targets, ordered steps,
// execution strategy. Nothing in execution should have to re-derive scope.
package planner

import (
	"fmt"

	"github.com/webdobe/dploy/internal/config"
	"github.com/webdobe/dploy/internal/environment"
	"github.com/webdobe/dploy/internal/operation"
)

// Plan is the materialized operation ready to execute.
type Plan struct {
	Operation   operation.Type
	Environment string
	Class       string
	Strategy    config.Strategy
	Targets     []TargetPlan
	Artifact    string
}

// TargetPlan is the per-target slice of a Plan.
type TargetPlan struct {
	Name  string
	Type  string
	Host  string
	Path  string
	Roles []string
	Steps []Step
}

// Step is one ordered command scheduled against a target.
type Step struct {
	Index   int
	Command string
}

// BuildDeploy expands a deploy Request against resolved env/config into a Plan.
func BuildDeploy(req operation.Request, cfg *config.Config, resolved *environment.Resolved) (*Plan, error) {
	envCfg := cfg.Environments[resolved.Name]
	return build(operation.TypeDeploy, envCfg.Deploy, req, resolved)
}

// BuildRollback expands a rollback Request against resolved env/config into a Plan.
// Returns an error if the environment has no rollback block defined —
// rollback is an explicit recovery operation, not an implicit inverse of deploy.
func BuildRollback(req operation.Request, cfg *config.Config, resolved *environment.Resolved) (*Plan, error) {
	envCfg := cfg.Environments[resolved.Name]
	if len(envCfg.Rollback) == 0 {
		return nil, fmt.Errorf("environment %q has no rollback steps defined", resolved.Name)
	}
	return build(operation.TypeRollback, envCfg.Rollback, req, resolved)
}

// build is the shared core of BuildDeploy / BuildRollback. opType labels
// the plan for audit; steps is the ordered step list to expand against
// each resolved target under on/on_role filters and --targets scoping.
func build(opType operation.Type, steps []config.Step, req operation.Request, resolved *environment.Resolved) (*Plan, error) {
	p := &Plan{
		Operation:   opType,
		Environment: resolved.Name,
		Class:       resolved.Class,
		Strategy:    resolved.Strategy,
		Artifact:    req.Artifact,
	}

	targets := resolved.Targets
	if len(req.Targets) > 0 {
		targets = environment.FilterByName(targets, req.Targets)
		if len(targets) == 0 {
			return nil, fmt.Errorf("no targets match requested names %v", req.Targets)
		}
	}

	for _, t := range targets {
		tp := TargetPlan{
			Name:  t.Name,
			Type:  t.Type,
			Host:  t.Host,
			Path:  t.Path,
			Roles: append([]string(nil), t.Roles...),
		}

		for i, step := range steps {
			if !stepMatches(step, t) {
				continue
			}
			tp.Steps = append(tp.Steps, Step{
				Index:   i,
				Command: step.Run,
			})
		}

		if len(tp.Steps) > 0 {
			p.Targets = append(p.Targets, tp)
		}
	}

	if len(p.Targets) == 0 {
		return nil, fmt.Errorf("plan has no executable steps against any target")
	}

	return p, nil
}

// stepMatches returns true if the step's on/on_role filters include the target.
// A step with no filters applies to all targets.
func stepMatches(step config.Step, t environment.Target) bool {
	if step.OnRole != "" {
		found := false
		for _, r := range t.Roles {
			if r == step.OnRole {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(step.On) > 0 {
		for _, name := range step.On {
			if name == t.Name {
				return true
			}
		}
		return false
	}
	return true
}
