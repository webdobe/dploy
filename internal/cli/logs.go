package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/webdobe/dploy/internal/state"
)

var logsCmd = &cobra.Command{
	Use:   "logs <environment>",
	Short: "Show logs for the last deploy of an environment",
	Long:  "Prints the recorded step output for the most recent operation against the environment.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := args[0]
		store := state.NewFileStore(filepath.Join(".dploy", "state"))
		result, err := store.Latest(env)
		if err != nil {
			return err
		}

		if result == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "No recorded logs for environment %q\n", env)
			return nil
		}

		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Logs for %s (operation=%s status=%s)\n\n", result.Environment, result.Type, result.Status)

		for i, step := range result.Steps {
			fmt.Fprintf(out, "=== step %d: %s\n", i+1, step.Command)
			fmt.Fprintf(out, "    target=%s status=%s exit=%d duration=%s\n", step.Target, step.Status, step.ExitCode, step.Duration)
			if step.Output != "" {
				trimmed := strings.TrimRight(step.Output, "\n")
				fmt.Fprintln(out, trimmed)
			}
			if step.Error != "" {
				fmt.Fprintf(out, "error: %s\n", step.Error)
			}
			fmt.Fprintln(out)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(logsCmd)
}
