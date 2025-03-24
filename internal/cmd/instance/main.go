// Package instance is the entrypoint of instance plugin
package instance

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/instance"
)

// NewCmd creates a new instance command
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instance",
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

			return instance.Start(cmd.Context())
		},
	}

	_ = viper.BindEnv("namespace", "NAMESPACE")
	_ = viper.BindEnv("pod-name", "POD_NAME")
	_ = viper.BindEnv("pgdata", "PGDATA")
	_ = viper.BindEnv("spool-directory", "SPOOL_DIRECTORY")

	return cmd
}
