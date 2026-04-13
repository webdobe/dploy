// Package cli defines the dploy command surface using Cobra.
//
// The CLI is a thin wrapper: it parses input, invokes the core runtime
// in other internal packages, and renders output for humans (or JSON).
// It must not own operation semantics, policy decisions, or planning logic.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/webdobe/dploy/internal/config"
	"github.com/webdobe/dploy/internal/failure"
	"github.com/webdobe/dploy/internal/operation"
	"github.com/webdobe/dploy/internal/planner"
)

// Global flags shared across commands.
var (
	configFile    string
	policyFile    string
	verbose       bool
	quiet         bool
	jsonOut       bool
	confirmFlag   bool
	sanitizedFlag bool
)

var rootCmd = &cobra.Command{
	Use:           "dploy",
	Short:         "A thin operations CLI for deploying code and moving environment data",
	Long:          "dploy connects your repo, servers, scripts, and existing tools into one clear workflow.\nIt does not try to replace them.",
	SilenceUsage:  true,
	SilenceErrors: true,
	// Treat `dploy <env>` as shorthand for `dploy up <env>`. The arg
	// only resolves to up when it names a configured environment;
	// anything else falls through to the normal "unknown command" error.
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		envName := args[0]
		cfg, err := config.Load(configFile)
		if err != nil {
			return fmt.Errorf("unknown command %q (and no %s to resolve it as an environment)", envName, configFile)
		}
		if _, ok := cfg.Environments[envName]; !ok {
			return fmt.Errorf("unknown command or environment %q", envName)
		}
		return runPipeline(cmd, envName, pipelineOp{
			opType:     operation.TypeDeploy,
			verb:       "Deploy",
			build:      planner.BuildDeploy,
			noPlanExit: failure.ExitInvalidConfig,
		})
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configFile, "file", "dploy.yml", "path to the dploy config file")
	rootCmd.PersistentFlags().StringVar(&policyFile, "policy", "/etc/dploy/policy.yml", "path to the trusted policy file (silently skipped if the default path is missing)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "print detailed output (commands, connection info, step-by-step execution)")
	rootCmd.PersistentFlags().BoolVar(&quiet, "quiet", false, "reduce output to essentials")
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output where supported")
	rootCmd.PersistentFlags().BoolVar(&confirmFlag, "confirm", false, "acknowledge policy 'confirm' requirements for this invocation")
	rootCmd.PersistentFlags().BoolVar(&sanitizedFlag, "sanitized", false, "assert data has been sanitized (satisfies policy 'sanitization' requirements)")
}

// Execute runs the root command. Callers (cmd/dploy/main.go) should exit
// non-zero if this returns an error.
func Execute() error {
	return rootCmd.Execute()
}
