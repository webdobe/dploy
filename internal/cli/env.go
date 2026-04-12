package cli

import (
	"github.com/spf13/cobra"
)

// envCmd is the parent for environment-level operations. It has no
// runtime behavior of its own — Cobra will print help when invoked
// without a subcommand.
var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Environment operations (sync, ...)",
	Long:  "Group of commands that act across environments. Currently: sync.",
}

func init() {
	rootCmd.AddCommand(envCmd)
}
