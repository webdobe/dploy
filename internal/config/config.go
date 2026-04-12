// Package config loads and validates the dploy.yml file.
//
// The config describes what the project wants to do. Trusted policy
// (loaded separately) describes what is allowed. Config must never be
// the sole authority for high-risk operations.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the parsed shape of dploy.yml.
type Config struct {
	App          string                 `yaml:"app"`
	Environments map[string]Environment `yaml:"environments"`
	Providers    *Providers             `yaml:"providers,omitempty"`
	Middleware   []string               `yaml:"middleware,omitempty"`
}

// Environment describes one named deploy target such as "staging" or "production".
type Environment struct {
	Class    string              `yaml:"class,omitempty"`
	Strategy Strategy            `yaml:"strategy,omitempty"`
	Targets  map[string]Target   `yaml:"targets"`
	Deploy   []Step              `yaml:"deploy,omitempty"`
	Notes    []string            `yaml:"notes,omitempty"` // printed after a successful `up`, never executed
	Rollback []Step              `yaml:"rollback,omitempty"`
	Secrets  *SecretsRef         `yaml:"secrets,omitempty"`
	Data     map[string][]string `yaml:"data,omitempty"`    // sync workflows, resource → commands
	Capture  map[string][]string `yaml:"capture,omitempty"` // capture workflows, resource → commands
	Restore  map[string][]string `yaml:"restore,omitempty"` // restore workflows, resource → commands
}

// Target is a single host or local path an environment runs against.
type Target struct {
	Type  string   `yaml:"type"`
	Host  string   `yaml:"host,omitempty"`
	Path  string   `yaml:"path"`
	Roles []string `yaml:"roles,omitempty"`
}

// Step is one ordered command in a deploy or rollback workflow.
type Step struct {
	Run    string   `yaml:"run"`
	On     []string `yaml:"on,omitempty"`
	OnRole string   `yaml:"on_role,omitempty"`
}

// SecretsRef points at a secrets provider. Values are never stored in config.
type SecretsRef struct {
	Provider string `yaml:"provider,omitempty"`
}

// Providers configures which extension providers dploy should load.
type Providers struct {
	Secrets  string `yaml:"secrets,omitempty"`
	Policy   string `yaml:"policy,omitempty"`
	Artifact string `yaml:"artifact,omitempty"`
}

// Strategy controls how targets within an environment are processed.
type Strategy string

const (
	StrategySequential Strategy = "sequential"
	StrategyParallel   Strategy = "parallel"
	StrategyRolling    Strategy = "rolling"
)

// TargetType values.
const (
	TargetLocal = "local"
	TargetSSH   = "ssh"
)

// Load reads and parses a dploy config file. It does not validate semantics —
// call Validate for that.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	return &cfg, nil
}

// Validate checks config semantics. Returns a slice of all errors found so
// the caller can report them together rather than one per run.
func Validate(cfg *Config) []error {
	var errs []error

	if cfg.App == "" {
		errs = append(errs, fmt.Errorf("app: required"))
	}
	if len(cfg.Environments) == 0 {
		errs = append(errs, fmt.Errorf("environments: at least one required"))
	}

	for name, env := range cfg.Environments {
		if len(env.Targets) == 0 {
			errs = append(errs, fmt.Errorf("environments.%s.targets: at least one required", name))
		}
		for tname, t := range env.Targets {
			switch t.Type {
			case TargetLocal:
				// ok
			case TargetSSH:
				if t.Host == "" {
					errs = append(errs, fmt.Errorf("environments.%s.targets.%s.host: required for ssh type", name, tname))
				}
			case "":
				errs = append(errs, fmt.Errorf("environments.%s.targets.%s.type: required", name, tname))
			default:
				errs = append(errs, fmt.Errorf("environments.%s.targets.%s.type: %q is not supported (want %q or %q)", name, tname, t.Type, TargetLocal, TargetSSH))
			}
			if t.Path == "" {
				errs = append(errs, fmt.Errorf("environments.%s.targets.%s.path: required", name, tname))
			}
		}
		for i, step := range env.Deploy {
			if step.Run == "" {
				errs = append(errs, fmt.Errorf("environments.%s.deploy[%d].run: required", name, i))
			}
		}
		switch env.Strategy {
		case "", StrategySequential, StrategyParallel, StrategyRolling:
			// ok
		default:
			errs = append(errs, fmt.Errorf("environments.%s.strategy: %q is not supported", name, env.Strategy))
		}
	}

	return errs
}
