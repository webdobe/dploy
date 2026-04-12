package cli

import (
	"github.com/spf13/cobra"

	"github.com/webdobe/dploy/internal/failure"
	"github.com/webdobe/dploy/internal/operation"
	"github.com/webdobe/dploy/internal/planner"
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback <environment>",
	Short: "Run rollback steps for an environment",
	Long: `Run the configured rollback steps for the named environment.

Only supported when the environment defines a rollback: block in
dploy.yml. Rollback is an explicit recovery operation — a failed deploy
does NOT trigger rollback automatically.

Exits non-zero with ExitRollbackUnavail (6) if no rollback is configured.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPipeline(cmd, args[0], pipelineOp{
			opType:     operation.TypeRollback,
			verb:       "Rollback",
			build:      planner.BuildRollback,
			noPlanExit: failure.ExitRollbackUnavail,
		})
	},
}

func init() {
	rootCmd.AddCommand(rollbackCmd)
}
