package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cmd/healthcheck"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cmd/instance"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cmd/operator"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cmd/restore"
)

func main() {
	cobra.EnableTraverseRunHooks = true

	logFlags := &log.Flags{}
	rootCmd := &cobra.Command{
		Use: "manager [cmd]",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			logFlags.ConfigureLogging()
			return nil
		},
	}

	logFlags.AddFlags(rootCmd.PersistentFlags())

	rootCmd.AddCommand(instance.NewCmd())
	rootCmd.AddCommand(operator.NewCmd())
	rootCmd.AddCommand(restore.NewCmd())
	rootCmd.AddCommand(healthcheck.NewCmd())

	if err := rootCmd.ExecuteContext(ctrl.SetupSignalHandler()); err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Println(err)
			os.Exit(1)
		}
	}
}
