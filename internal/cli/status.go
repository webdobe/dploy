package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/webdobe/dploy/internal/state"
)

var statusCmd = &cobra.Command{
	Use:   "status <environment>",
	Short: "Show the last known deploy state for an environment",
	Long:  "Reads local state from .dploy/state/ only in v1.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := args[0]
		store := state.NewFileStore(filepath.Join(".dploy", "state"))
		result, err := store.Latest(env)
		if err != nil {
			return err
		}

		if result == nil {
			if jsonOut {
				b, _ := json.Marshal(map[string]string{"environment": env, "status": "unknown"})
				fmt.Fprintln(cmd.OutOrStdout(), string(b))
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "No recorded state for environment %q\n", env)
			return nil
		}

		if jsonOut {
			b, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		}

		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Environment: %s\n", result.Environment)
		fmt.Fprintf(out, "Operation:   %s\n", result.Type)
		fmt.Fprintf(out, "Status:      %s\n", result.Status)
		fmt.Fprintf(out, "Started:     %s\n", result.StartedAt.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(out, "Finished:    %s\n", result.FinishedAt.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(out, "Steps:       %d\n", len(result.Steps))
		if result.Error != "" {
			fmt.Fprintf(out, "Error:       %s\n", result.Error)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
