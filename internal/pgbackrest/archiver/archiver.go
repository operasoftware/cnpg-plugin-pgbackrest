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

// Package archiver manages the WAL archiving process
package archiver

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"

	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	pgbackrestCommand "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/command"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/spool"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/walarchive"
)

// WALArchiver is a structure containing every info need to archive a set of WAL files
// using pgbackrest archive-push
type WALArchiver struct {
	// The spool of WAL files to be archived in parallel
	spool *spool.WALSpool

	// The environment that should be used to invoke pgbackrest archive-push
	env []string

	pgDataDirectory string

	// this should become a grpc interface
	pgbackrestArchiver *walarchive.PgbackrestArchiver
}

// WALArchiverResult contains the result of the archival of one WAL
type WALArchiverResult struct {
	// The WAL that have been archived
	WalName string

	// If not nil, this is the error that has been detected
	Err error

	// The time when we started pgbackrest archive-push
	StartTime time.Time

	// The time when pgbackrest archive-push ended
	EndTime time.Time
}

// New creates a new WAL archiver
func New(
	ctx context.Context,
	env []string,
	spoolDirectory string,
	pgDataDirectory string,
	emptyWalArchivePath string,
) (archiver *WALArchiver, err error) {
	contextLog := log.FromContext(ctx)
	var walArchiveSpool *spool.WALSpool

	if walArchiveSpool, err = spool.New(spoolDirectory); err != nil {
		contextLog.Info("Cannot initialize the WAL spool", "spoolDirectory", spoolDirectory)
		return nil, fmt.Errorf("while creating spool directory: %w", err)
	}

	archiver = &WALArchiver{
		spool:           walArchiveSpool,
		env:             env,
		pgDataDirectory: pgDataDirectory,
		pgbackrestArchiver: &walarchive.PgbackrestArchiver{
			Env:                 env,
			Touch:               walArchiveSpool.Touch,
			EmptyWalArchivePath: emptyWalArchivePath,
		},
	}
	return archiver, nil
}

// DeleteFromSpool checks if a WAL file is in the spool and, if it is, remove it
func (archiver *WALArchiver) DeleteFromSpool(
	walName string,
) (hasBeenDeleted bool, err error) {
	var isContained bool

	// this code assumes the wal-archive command is run at most once at each instant,
	// given that PostgreSQL will call it sequentially without overlapping
	isContained, err = archiver.spool.Contains(walName)
	if !isContained || err != nil {
		return false, err
	}

	return true, archiver.spool.Remove(walName)
}

// ArchiveList archives a list of WAL files in parallel
func (archiver *WALArchiver) ArchiveList(
	ctx context.Context,
	walNames []string,
	options []string,
) (result []WALArchiverResult) {
	res := archiver.pgbackrestArchiver.ArchiveList(
		ctx,
		walNames,
		options,
	)
	for _, re := range res {
		result = append(result, WALArchiverResult{
			WalName:   re.WalName,
			Err:       re.Err,
			StartTime: re.StartTime,
			EndTime:   re.EndTime,
		})
	}
	return result
}

// CheckWalArchiveDestination checks if the destination archive is ready to perform
// archiving, i.e. if proper stanzas exist.
func (archiver *WALArchiver) CheckWalArchiveDestination(
	ctx context.Context,
	configuration *pgbackrestApi.PgbackrestConfiguration,
	stanza string,
	env []string,
) error {
	// Probably the easiest way to check if stanza exists is to run "pgbackrest info".
	// It's possible to use stanza-create instead but it requires the lock file
	// which makes it unusable during backups.
	_, err := pgbackrestCommand.GetBackupList(ctx, configuration, stanza, env)
	return err
}

// PgbackrestCheckWalArchiveOptions create the options needed for the `pgbackrest check`
// command.
func (archiver *WALArchiver) PgbackrestCheckWalArchiveOptions(
	ctx context.Context,
	configuration *pgbackrestApi.PgbackrestConfiguration,
	clusterName string,
) ([]string, error) {
	var options []string

	options, err := pgbackrestCommand.AppendCloudProviderOptionsFromConfiguration(ctx, options, configuration)
	if err != nil {
		return nil, err
	}

	options, err = pgbackrestCommand.AppendStanzaOptionsFromConfiguration(
		ctx,
		options,
		configuration,
		archiver.pgDataDirectory,
		true,
	)
	if err != nil {
		return nil, err
	}

	options, err = pgbackrestCommand.AppendLogOptionsFromConfiguration(
		ctx,
		options,
		configuration,
	)
	if err != nil {
		return nil, err
	}

	stanza := clusterName
	if len(configuration.Stanza) != 0 {
		stanza = configuration.Stanza
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
