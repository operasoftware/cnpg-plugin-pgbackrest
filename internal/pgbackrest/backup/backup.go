/*
Copyright The CloudNativePG Contributors
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

package backup

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/blang/semver"
	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/log"

	cnpgApiV1 "github.com/cloudnative-pg/api/pkg/api/v1"
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	pgbackrestCatalog "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/catalog"
	pgbackrestCommand "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/command"
)

// Command represents a pgbackrest backup command
type Command struct {
	configuration   *pgbackrestApi.PgbackrestConfiguration
	backupConfig    *cnpgApiV1.BackupPluginConfiguration
	pgDataDirectory string
}

// NewBackupCommand creates a new pgbackrest backup command
func NewBackupCommand(
	configuration *pgbackrestApi.PgbackrestConfiguration,
	backupConfig *cnpgApiV1.BackupPluginConfiguration,
	pgDataDirectory string,
) *Command {
	return &Command{
		configuration:   configuration,
		backupConfig:    backupConfig,
		pgDataDirectory: pgDataDirectory,
	}
}

// GetDataConfiguration gets the configuration in the `Data` object of the pgbackrest configuration
func (b *Command) GetDataConfiguration(
	options []string,
) ([]string, error) {
	if b.configuration.Data == nil {
		return options, nil
	}

	if len(b.configuration.Compression) != 0 {
		options = append(
			options,
			"--compress-type",
			string(b.configuration.Compression))
	}

	// TODO: Add per-repo options
	// for index, repo := range b.configuration.Repositories {
	//   options = append(options,)
	// }

	if b.configuration.Data.ImmediateCheckpoint {
		options = append(
			options,
			"--start-fast")
	}

	if b.configuration.Data.Jobs != nil {
		options = append(
			options,
			"--process-max",
			strconv.Itoa(int(*b.configuration.Data.Jobs)))
	}

	return b.configuration.Data.AppendAdditionalCommandArgs(options), nil
}

// GetPgbackrestBackupOptions extract the list of command line options to be used with
// pgbackrest backup
func (b *Command) GetPgbackrestBackupOptions(
	ctx context.Context,
	backupName string,
	stanza string,
) ([]string, error) {
	options := []string{
		"backup",
		"--annotation", fmt.Sprintf("%s=%s", pgbackrestCatalog.BackupNameAnnotation, backupName),
	}

	options, err := b.GetDataConfiguration(options)
	if err != nil {
		return nil, err
	}

	if b.configuration.Data != nil {
		for k, v := range b.configuration.Data.Annotations {
			if k == pgbackrestCatalog.BackupNameAnnotation {
				err = fmt.Errorf(
					"annotation '%s' is reserved for backup name",
					pgbackrestCatalog.BackupNameAnnotation,
				)
				return nil, err
			}
			options = append(
				options,
				"--annotation",
				fmt.Sprintf("%s=%s", k, v),
			)
		}
	}

	options, err = pgbackrestCommand.AppendCloudProviderOptionsFromConfiguration(ctx, options, b.configuration)
	if err != nil {
		return nil, err
	}

	options, err = pgbackrestCommand.AppendRetentionOptionsFromConfiguration(ctx, options, b.configuration)
	if err != nil {
		return nil, err
	}

	options, err = pgbackrestCommand.AppendStanzaOptionsFromConfiguration(ctx, options, b.configuration, b.pgDataDirectory, true)
	if err != nil {
		return nil, err
	}

	options, err = pgbackrestCommand.AppendLogOptionsFromConfiguration(ctx, options, b.configuration)
	if err != nil {
		return nil, err
	}

	if b.backupConfig.Parameters != nil {
		for k, v := range b.backupConfig.Parameters {
			options = append(
				options,
				fmt.Sprintf("--%s=%s", k, v),
			)
		}
	}

	options = append(
		options,
		"--stanza",
		stanza,
		"--lock-path",
		"/controller/tmp/pgbackrest",
		"--no-archive-check",
	)

	return options, nil
}

// GetStanzaCreateOptions extract the list of command line options to be used with
// pgbackrest stanza-create
func (b *Command) getStanzaCreateOptions(
	ctx context.Context,
	stanza string,
) ([]string, error) {
	options := []string{
		"stanza-create",
	}

	options, err := pgbackrestCommand.AppendCloudProviderOptionsFromConfiguration(ctx, options, b.configuration)
	if err != nil {
		return nil, err
	}

	options, err = pgbackrestCommand.AppendStanzaOptionsFromConfiguration(ctx, options, b.configuration, b.pgDataDirectory, true)
	if err != nil {
		return nil, err
	}

	options, err = pgbackrestCommand.AppendLogOptionsFromConfiguration(ctx, options, b.configuration)
	if err != nil {
		return nil, err
	}

	options = append(
		options,
		"--stanza",
		stanza,
		"--lock-path",
		"/controller/tmp/pgbackrest",
	)

	return options, nil
}

// GetExecutedBackupInfo get the status information about the executed backup
func (b *Command) GetExecutedBackupInfo(
	ctx context.Context,
	backupName string,
	stanza string,
	env []string,
) (*pgbackrestCatalog.Catalog, error) {
	return pgbackrestCommand.GetBackupByAnnotatedName(
		ctx,
		backupName,
		stanza,
		b.configuration,
		env,
	)
}

// IsCompatible checks if pgbackrest can back up this version of PostgreSQL
func (b *Command) IsCompatible(postgresVers semver.Version) error {
	// Pgbackrest supports latest 10 major Postgres versions while operator itself
	// only supports currently maintained releases, i.e. 5 or so.
	// That means it should be safe to skip this check completely.
	return nil
}

// Take takes a backup
func (b *Command) Take(
	ctx context.Context,
	backupName string,
	stanza string,
	env []string,
	backupTemporaryDirectory string,
) error {
	log := log.FromContext(ctx)

	options, backupErr := b.GetPgbackrestBackupOptions(ctx, backupName, stanza)
	if backupErr != nil {
		log.Error(backupErr, "while getting pgbackrest backup options")
		return backupErr
	}

	// record the backup beginning
	log.Info("Starting pgbackrest backup", "options", options)

	cmd := exec.Command("pgbackrest", options...) // #nosec G204
	cmd.Env = env
	// TODO: Should tmpdir be handled differently in pgbackrest?
	cmd.Env = append(cmd.Env, "TMPDIR="+backupTemporaryDirectory)
	if err := execlog.RunStreaming(cmd, "pgbackrest backup"); err != nil {
		const badArgumentsErrorCode = "3"
		if err.Error() == badArgumentsErrorCode {
			descriptiveError := errors.New("invalid arguments for pgbackrest backup. " +
				"Ensure that the additionalCommandArgs field is correctly populated")
			log.Error(descriptiveError, "error while executing pgbackrest backup",
				"arguments", options)
			return descriptiveError
		}
		return err
	}

	log.Info("Completed pgbackrest backup", "options", options)

	return nil
}

// CreatePgbackrestStanza ensures that the destinationArchive is ready to perform archiving.
// It's safe to re-run pgbackrest stanza-create on an existing archive, command will
// fail if another database was used to create it.
func (b *Command) CreatePgbackrestStanza(ctx context.Context, stanza string, env []string) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("pgbackrest creating stanza")

	options, err := b.getStanzaCreateOptions(ctx, stanza)
	if err != nil {
		return err
	}

	contextLogger.Info(
		"Executing pgbackrest stanza-create command",
		"options", options,
	)

	stanzaCreateCmd := exec.Command("pgbackrest", options...) // #nosec G204
	stanzaCreateCmd.Env = env

	err = execlog.RunStreaming(stanzaCreateCmd, "pgbackrest stanza-create")
	if err != nil {
		contextLogger.Error(err, "Error invoking pgbackrest stanza-create",
			"options", options,
			"exitCode", stanzaCreateCmd.ProcessState.ExitCode(),
		)
		return fmt.Errorf("unexpected failure invoking pgbackrest stanza-create: %w", err)
	}

	contextLogger.Trace("pgbackrest stanza-create command execution completed")

	return nil
}
