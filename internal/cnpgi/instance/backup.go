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
	"fmt"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cnpg-i-machinery/pkg/pluginhelper/decoder"
	"github.com/cloudnative-pg/cnpg-i/pkg/backup"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/common"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/operator/config"
	pgbackrestBackup "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/backup"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/catalog"
	pgbackrestCredentials "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/credentials"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/utils"
)

// BackupServiceImplementation is the implementation
// of the Backup CNPG capability
type BackupServiceImplementation struct {
	Client       client.Client
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

	backupConfig, err := decoder.DecodeBackup(request.BackupDefinition)
	if err != nil {
		contextLogger.Error(err, "while getting backup definition")
		return nil, err
	}

	configuration, err := config.NewFromClusterJSON(request.ClusterDefinition)
	if err != nil {
		return nil, err
	}

	var archive pgbackrestv1.Archive
	if err := b.Client.Get(ctx, configuration.GetArchiveObjectKey(), &archive); err != nil {
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

	// We need to connect to PostgreSQL and to do that we need
	// PGHOST (and the like) to be available
	osEnvironment := utils.SanitizedEnviron()
	caBundleEnvironment := common.GetRestoreCABundleEnv(&archive.Spec.Configuration)
	env, err := pgbackrestCredentials.EnvSetBackupCloudCredentials(
		ctx,
		b.Client,
		archive.Namespace,
		&archive.Spec.Configuration,
		common.MergeEnv(osEnvironment, caBundleEnvironment))
	if err != nil {
		contextLogger.Error(err, "while setting backup cloud credentials")
		return nil, err
	}

	err = backupCmd.CreatePgbackrestStanza(ctx, configuration.Stanza, env)
	if err != nil {
		contextLogger.Error(err, "while initializing pgbackrest stanza")
		return nil, err
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
		Metadata: map[string]string{
			"version":     metadata.Data.Version,
			"name":        metadata.Data.Name,
			"displayName": metadata.Data.DisplayName,
		},
	}, nil
}
