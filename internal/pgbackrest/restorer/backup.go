/*
Copyright 2025, Opera Norway AS

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package restorer

import (
	"context"
	"errors"
	"os/exec"
	"strconv"

	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/log"

	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	pgbackrestCommand "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/command"
)

// Command represents a pgbackrest restore command
type Command struct {
	configuration   *pgbackrestApi.PgbackrestConfiguration
	pgDataDirectory string
}

// NewRestoreCommand creates a new pgbackrest restore command
func NewRestoreCommand(
	configuration *pgbackrestApi.PgbackrestConfiguration,
	pgDataDirectory string,
) *Command {
	return &Command{
		configuration:   configuration,
		pgDataDirectory: pgDataDirectory,
	}
}

// GetRestoreConfiguration gets the configuration in the `Restore` object of the pgbackrest configuration
func (b *Command) GetRestoreConfiguration(
	options []string,
) ([]string, error) {
	if b.configuration.Restore == nil {
		return options, nil
	}
	if b.configuration.Restore.Jobs != nil {
		options = append(
			options,
			"--process-max",
			strconv.Itoa(int(*b.configuration.Restore.Jobs)))
	}

	return b.configuration.Restore.AppendAdditionalRestoreCommandArgs(options), nil
}

// GetPgbackrestRestoreOptions extract the list of command line options to be used with
// pgbackrest restore
func (b *Command) GetPgbackrestRestoreOptions(
	ctx context.Context,
	backupName string,
	stanza string,
) ([]string, error) {
	var options []string

	options = append(
		options,
		"--stanza", stanza,
		"--lock-path",
		"/controller/tmp/pgbackrest")

	options, err := b.GetRestoreConfiguration(options)
	if err != nil {
		return nil, err
	}

	options, err = pgbackrestCommand.AppendCloudProviderOptionsFromConfiguration(ctx, options, b.configuration)
	if err != nil {
		return nil, err
	}

	options, err = pgbackrestCommand.AppendLogOptionsFromConfiguration(ctx, options, b.configuration)
	if err != nil {
		return nil, err
	}

	options, err = pgbackrestCommand.AppendStanzaOptionsFromConfiguration(
		ctx,
		options,
		b.configuration,
		b.pgDataDirectory,
		false,
	)
	if err != nil {
		return nil, err
	}

	options = append(
		options,
		"restore",
		"--set",
		backupName,
	)

	return options, nil
}

// Restore restores a database from backup
func (b *Command) Restore(
	ctx context.Context,
	backupName string,
	stanza string,
	env []string,
) error {
	log := log.FromContext(ctx)

	options, err := b.GetPgbackrestRestoreOptions(ctx, backupName, stanza)
	if err != nil {
		log.Error(err, "while getting pgbackrest restore options")
		return err
	}

	log.Info("Starting pgbackrest restore", "options", options)

	cmd := exec.Command("pgbackrest", options...) // #nosec G204
	cmd.Env = env
	err = execlog.RunStreaming(cmd, "pgbackrest restore")
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			err = pgbackrestCommand.UnmarshalPgbackrestRestoreExitCode(ctx, exitError.ExitCode())
		}

		log.Error(err, "Can't restore backup")
		return err
	}
	log.Info("Restore completed")
	return nil
}
