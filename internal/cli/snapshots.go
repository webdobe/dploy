package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/webdobe/dploy/internal/state"
)

var snapshotsCmd = &cobra.Command{
	Use:   "snapshots <environment>",
	Short: "List captured snapshots for an environment",
	Long: `List the snapshots recorded under .dploy/snapshots/<env>/ by previous
'dploy capture' runs. Newest first.

With --json, emits the full snapshot metadata array for scripting.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env := args[0]
		store := state.NewFileSnapshotStore(filepath.Join(".dploy", "snapshots"))
		snaps, err := store.List(env)
		if err != nil {
			return err
		}

		if jsonOut {
			if snaps == nil {
				snaps = []*state.Snapshot{}
			}
			b, err := json.MarshalIndent(snaps, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		}

		out := cmd.OutOrStdout()
		if len(snaps) == 0 {
			fmt.Fprintf(out, "No snapshots for environment %q\n", env)
			return nil
		}

		fmt.Fprintf(out, "%-44s  %-16s  %-20s  %s\n", "ID", "STATUS", "CREATED", "RESOURCES")
		for _, s := range snaps {
			resources := strings.Join(s.Resources, ",")
			if s.Sanitized {
				resources += " (sanitized)"
			}
			fmt.Fprintf(out, "%-44s  %-16s  %-20s  %s\n",
				s.ID,
				s.Status,
				s.CreatedAt.Format("2006-01-02 15:04:05"),
				resources,
			)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(snapshotsCmd)
}
