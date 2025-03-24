// Package restore is the entrypoint of restore capabilities
package restore

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/restore"
)

// NewCmd creates the "restore" subcommand
func NewCmd() *cobra.Command {
	cobra.EnableTraverseRunHooks = true

	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Starts the Pgbackrest CNPG-I sidecar plugin",
		RunE: func(cmd *cobra.Command, _ []string) error {
			requiredSettings := []string{
				"namespace",
				"pod-name",
				"spool-directory",
			}

			for _, k := range requiredSettings {
				if len(viper.GetString(k)) == 0 {
					return fmt.Errorf("missing required %s setting", k)
				}
			}

			return restore.Start(cmd.Context())
		},
	}

	_ = viper.BindEnv("namespace", "NAMESPACE")
	_ = viper.BindEnv("pod-name", "POD_NAME")
	_ = viper.BindEnv("pgdata", "PGDATA")
	_ = viper.BindEnv("spool-directory", "SPOOL_DIRECTORY")

	return cmd
}
