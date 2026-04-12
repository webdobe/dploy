// Package cli defines the dploy command surface using Cobra.
//
// The CLI is a thin wrapper: it parses input, invokes the core runtime
// in other internal packages, and renders output for humans (or JSON).
// It must not own operation semantics, policy decisions, or planning logic.
package cli

import (
	"github.com/spf13/cobra"
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
