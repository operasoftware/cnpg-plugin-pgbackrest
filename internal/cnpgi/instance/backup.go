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

package instance

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cnpg-i-machinery/pkg/pluginhelper/decoder"
	"github.com/cloudnative-pg/cnpg-i/pkg/backup"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/operator/config"
	pgbackrestBackup "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/backup"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/catalog"
	pgbackrestCommand "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/command"
	pgbackrestCredentials "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/credentials"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/utils"
)

const (
	standbyServerReachableTimeout  = 90 * time.Second
	standbyServerReachableInterval = 2 * time.Second
	standbyServerDialTimeout       = 3 * time.Second
)

// BackupServiceImplementation is the implementation
// of the Backup CNPG capability
type BackupServiceImplementation struct {
	Client       client.Client
	APIReader    client.Reader
	InstanceName string
	PGDataPath   string
	backup.UnimplementedBackupServer
}

// GetCapabilities implements the BackupService interface
func (b BackupServiceImplementation) GetCapabilities(
	_ context.Context, _ *backup.BackupCapabilitiesRequest,
) (*backup.BackupCapabilitiesResult, error) {
	return &backup.BackupCapabilitiesResult{
		Capabilities: []*backup.BackupCapability{
			{
				Type: &backup.BackupCapability_Rpc{
					Rpc: &backup.BackupCapability_RPC{
						Type: backup.BackupCapability_RPC_TYPE_BACKUP,
					},
				},
			},
		},
	}, nil
}

// Backup implements the Backup interface
func (b BackupServiceImplementation) Backup(
	ctx context.Context,
	request *backup.BackupRequest,
) (*backup.BackupResult, error) {
	contextLogger := log.FromContext(ctx)

	contextLogger.Info("Starting backup")

	backupConfig, err := decoder.DecodeBackupLenient(request.BackupDefinition)
	if err != nil {
		contextLogger.Error(err, "while getting backup definition")
		return nil, err
	}

	configuration, err := config.NewFromClusterJSON(request.ClusterDefinition)
	if err != nil {
		return nil, err
	}

	var archive pgbackrestv1.Archive
	// Uncached: right after backupStandby is enabled the cache can still hold the disabled Archive.
	if err := b.APIReader.Get(ctx, configuration.GetArchiveObjectKey(), &archive); err != nil {
		contextLogger.Error(err, "while getting archive", "key", configuration.GetRecoveryArchiveObjectKey())
		return nil, err
	}

	if err := fileutils.EnsureDirectoryExists(postgres.BackupTemporaryDirectory); err != nil {
		contextLogger.Error(err, "Cannot create backup temporary directory", "err", err)
		return nil, err
	}

	backupCmd := pgbackrestBackup.NewBackupCommand(
		&archive.Spec.Configuration,
		backupConfig.Spec.PluginConfiguration,
		b.PGDataPath,
	)

	currentPrimary := configuration.Cluster.Status.CurrentPrimary
	onStandby, err := checkBackupTarget(
		archive.Spec.Configuration.IsBackupStandbyEnabled(), currentPrimary, b.InstanceName)
	if err != nil {
		contextLogger.Error(err, "while checking the backup target")
		return nil, err
	}
	if onStandby {
		if err := pgbackrestCommand.ValidateStandbyStanza(configuration.Stanza); err != nil {
			contextLogger.Error(err, "while checking the stanza for a backup from a standby")
			return nil, err
		}
		host := archive.Spec.Configuration.BackupStandby.GetServiceName(configuration.Cluster.Name)
		address := net.JoinHostPort(host, strconv.Itoa(pgbackrestCommand.StandbyServerPort))
		if err := waitForStandbyServer(
			ctx, address, standbyServerReachableTimeout, standbyServerReachableInterval,
		); err != nil {
			contextLogger.Error(err, "while waiting for the pgBackRest TLS server of the primary")
			return nil, err
		}
		contextLogger.Info("Taking backup from standby", "host", host)
		backupCmd = backupCmd.WithStandbyPrimary(host)
	}

	// We need to connect to PostgreSQL and to do that we need
	// PGHOST (and the like) to be available
	osEnvironment := utils.SanitizedEnviron()
	env, err := pgbackrestCredentials.EnvSetBackupCloudCredentials(
		ctx,
		b.Client,
		archive.Namespace,
		&archive.Spec.Configuration,
		osEnvironment)
	if err != nil {
		contextLogger.Error(err, "while setting backup cloud credentials")
		return nil, err
	}

	// Create the stanza unless the Archive disables it (createStanza=Disabled), in which
	// case it is expected to be managed out of band.
	if archive.Spec.Configuration.ShouldCreateStanzaOnBackup() {
		if err = backupCmd.CreatePgbackrestStanza(ctx, configuration.Stanza, env); err != nil {
			contextLogger.Error(err, "while initializing pgbackrest stanza")
			return nil, err
		}
	}

	backupName := fmt.Sprintf("backup-%v", pgTime.ToCompactISO8601(time.Now()))

	if err = backupCmd.Take(
		ctx,
		backupName,
		configuration.Stanza,
		env,
		postgres.BackupTemporaryDirectory,
	); err != nil {
		contextLogger.Error(err, "while taking backup")
		return nil, err
	}

	executedBackupInfo, err := backupCmd.GetExecutedBackupInfo(
		ctx,
		backupName,
		configuration.Stanza,
		env)
	if err != nil {
		contextLogger.Error(err, "while getting executed backup info")
		return nil, err
	}

	contextLogger.Info("Backup completed", "backup", executedBackupInfo.Backups[0].ID)
	return &backup.BackupResult{
		BackupId:   executedBackupInfo.Backups[0].ID,
		BackupName: executedBackupInfo.Backups[0].Annotations[catalog.BackupNameAnnotation],
		StartedAt:  executedBackupInfo.Backups[0].Time.Start,
		StoppedAt:  executedBackupInfo.Backups[0].Time.Stop,
		BeginWal:   executedBackupInfo.Backups[0].WAL.Start,
		EndWal:     executedBackupInfo.Backups[0].WAL.Stop,
		BeginLsn:   executedBackupInfo.Backups[0].LSN.Start,
		EndLsn:     executedBackupInfo.Backups[0].LSN.Stop,
		InstanceId: b.InstanceName,
		Online:     true,
		Metadata: newBackupResultMetadata(
			configuration.Cluster.UID, configuration.Stanza, &archive.Spec.Configuration).toMap(),
	}, nil
}

// checkBackupTarget reports whether the backup runs on a standby and rejects the
// combinations that cannot work
func checkBackupTarget(backupStandbyEnabled bool, currentPrimary, instanceName string) (bool, error) {
	if currentPrimary == instanceName {
		return false, nil
	}
	if !backupStandbyEnabled {
		if currentPrimary == "" {
			return false, nil
		}
		return false, fmt.Errorf(
			"the backup was dispatched to the standby %q but backupStandby is not enabled in the Archive: "+
				"enable it or set the backup target to primary", instanceName)
	}
	if currentPrimary == "" {
		return false, errors.New("cluster has no current primary")
	}
	return true, nil
}

// waitForStandbyServer waits until the TLS server of the primary accepts connections:
// a backup can be dispatched before the primary's sidecar has started it.
// This only checks reachability, not TLS or authorisation.
func waitForStandbyServer(ctx context.Context, address string, timeout, interval time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := net.Dialer{Timeout: standbyServerDialTimeout}
	var lastErr error
	for {
		conn, err := dialer.DialContext(waitCtx, "tcp", address)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		// A dial cut short by the deadline says nothing about the server.
		if waitCtx.Err() == nil {
			lastErr = err
		}

		select {
		case <-waitCtx.Done():
		case <-time.After(interval):
			continue
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("no connection attempt completed within %s", timeout)
		}
		return fmt.Errorf(
			"the pgBackRest TLS server of the primary is not reachable at %s: %w. "+
				"The Service must select the primary and expose port 8432; "+
				"after enabling backupStandby the primary may still be waiting for its next configuration refresh",
			address, lastErr)
	}
}
