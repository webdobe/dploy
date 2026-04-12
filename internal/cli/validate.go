package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/webdobe/dploy/internal/config"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the config file",
	Long: `Validate that the dploy config file exists, parses, and declares
the required fields and step shapes.

Does not connect to any hosts.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configFile)
		if err != nil {
			return err
		}

		if errs := config.Validate(cfg); len(errs) > 0 {
			for _, e := range errs {
				fmt.Fprintf(cmd.ErrOrStderr(), "  - %s\n", e)
			}
			return fmt.Errorf("config is invalid: %d error(s)", len(errs))
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Config is valid: %s\n", configFile)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
