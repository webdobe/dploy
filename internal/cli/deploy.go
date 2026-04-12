package cli

import (
	"github.com/spf13/cobra"

	"github.com/webdobe/dploy/internal/failure"
	"github.com/webdobe/dploy/internal/operation"
	"github.com/webdobe/dploy/internal/planner"
)

var deployCmd = &cobra.Command{
	Use:   "up <environment>",
	Short: "Run deploy steps for an environment",
	Long: `Run the configured deploy steps against the named environment.

Behavior:
  - loads config file
  - validates environment exists
  - checks trusted policy
  - connects locally or over SSH
  - changes into the configured path
  - runs deploy steps in order
  - streams output
  - exits on first failure
  - stores deploy result under .dploy/state/`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPipeline(cmd, args[0], pipelineOp{
			opType:     operation.TypeDeploy,
			verb:       "Deploy",
			build:      planner.BuildDeploy,
			noPlanExit: failure.ExitInvalidConfig,
		})
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)
}
