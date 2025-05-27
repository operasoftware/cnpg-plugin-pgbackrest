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

package restore

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	restore "github.com/cloudnative-pg/cnpg-i/pkg/restore/job"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/operator/config"
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	pgbackrestArchiver "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/archiver"
	pgbackrestCatalog "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/catalog"
	pgbackrestCommand "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/command"
	pgbackrestCredentials "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/credentials"
	pgbackrestRestorer "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/restorer"
	pgbackrestUtils "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/utils"
)

const (
	// ScratchDataDirectory is the directory to be used for scratch data
	ScratchDataDirectory = "/controller"

	// RecoveryTemporaryDirectory provides a path to store temporary files
	// needed in the recovery process
	RecoveryTemporaryDirectory = ScratchDataDirectory + "/recovery"
)

// JobHookImpl is the implementation of the restore job hooks
type JobHookImpl struct {
	restore.UnimplementedRestoreJobHooksServer

	Client client.Client

	SpoolDirectory       string
	PgDataPath           string
	PgWalFolderToSymlink string
}

// GetCapabilities returns the capabilities of the restore job hooks
func (impl JobHookImpl) GetCapabilities(
	_ context.Context,
	_ *restore.RestoreJobHooksCapabilitiesRequest,
) (*restore.RestoreJobHooksCapabilitiesResult, error) {
	return &restore.RestoreJobHooksCapabilitiesResult{
		Capabilities: []*restore.RestoreJobHooksCapability{
			{
				Kind: restore.RestoreJobHooksCapability_KIND_RESTORE,
			},
		},
	}, nil
}

// Restore restores the cluster from a backup
func (impl JobHookImpl) Restore(
	ctx context.Context,
	req *restore.RestoreRequest,
) (*restore.RestoreResponse, error) {
	contextLogger := log.FromContext(ctx)

	configuration, err := config.NewFromClusterJSON(req.ClusterDefinition)
	if err != nil {
		return nil, err
	}

	var recoveryArchive pgbackrestv1.Archive
	if err := impl.Client.Get(ctx, configuration.GetRecoveryArchiveObjectKey(), &recoveryArchive); err != nil {
		return nil, err
	}

	if configuration.PgbackrestObjectName != "" {
		var targeArchive pgbackrestv1.Archive
		if err := impl.Client.Get(ctx, configuration.GetArchiveObjectKey(), &targeArchive); err != nil {
			return nil, err
		}

		if err := impl.checkBackupDestination(ctx, configuration.Cluster, &targeArchive.Spec.Configuration); err != nil {
			return nil, err
		}
	}

	// Detect the backup to recover
	backup, env, err := loadBackupObjectFromExternalCluster(
		ctx,
		impl.Client,
		configuration.Cluster,
		&recoveryArchive.Spec.Configuration,
		configuration.RecoveryStanza,
	)
	if err != nil {
		return nil, err
	}

	if err := impl.restoreDataDir(
		ctx,
		backup,
		env,
		&recoveryArchive.Spec.Configuration,
	); err != nil {
		return nil, err
	}

	if configuration.Cluster.Spec.WalStorage != nil {
		if _, err := impl.restoreCustomWalDir(ctx); err != nil {
			return nil, err
		}
	}

	config := getRestoreWalConfig()

	contextLogger.Info("sending restore response", "config", config)
	return &restore.RestoreResponse{
		RestoreConfig: config,
		Envs:          nil,
	}, nil
}

// restoreDataDir restores PGDATA from an existing backup
func (impl JobHookImpl) restoreDataDir(
	ctx context.Context,
	backup *cnpgv1.Backup,
	env []string,
	pgbackrestConfiguration *pgbackrestApi.PgbackrestConfiguration,
) error {
	restoreCmd := pgbackrestRestorer.NewRestoreCommand(
		pgbackrestConfiguration,
		impl.PgDataPath,
	)

	return restoreCmd.Restore(ctx, backup.Status.BackupID, backup.Status.ServerName, env)
}

// TODO: Likely doesn't make sense for pgbackrest. Might be tricky to implement properly.
// nolint: unused
func (impl JobHookImpl) ensureArchiveContainsLastCheckpointRedoWAL(
	ctx context.Context,
	env []string,
	backup *cnpgv1.Backup,
	pgbackrestConfiguration *pgbackrestApi.PgbackrestConfiguration,
) error {
	// it's the full path of the file that will temporarily contain the LastCheckpointRedoWAL
	const testWALPath = RecoveryTemporaryDirectory + "/test.wal"
	contextLogger := log.FromContext(ctx)

	defer func() {
		if err := fileutils.RemoveFile(testWALPath); err != nil {
			contextLogger.Error(err, "while deleting the temporary wal file: %w")
		}
	}()

	if err := fileutils.EnsureParentDirectoryExists(testWALPath); err != nil {
		return err
	}

	rest, err := pgbackrestRestorer.NewWALRestorer(
		ctx,
		env,
		impl.SpoolDirectory,
	)
	if err != nil {
		return err
	}

	opts, err := pgbackrestCommand.CloudWalRestoreOptions(
		ctx,
		pgbackrestConfiguration,
		backup.Status.ServerName,
		testWALPath,
	)
	if err != nil {
		return err
	}

	if err := rest.Restore(ctx, backup.Status.BeginWal, testWALPath, opts); err != nil {
		return fmt.Errorf("encountered an error while checking the presence of first needed WAL in the archive: %w", err)
	}

	return nil
}

func (impl *JobHookImpl) checkBackupDestination(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
	pgbackrestConfiguration *pgbackrestApi.PgbackrestConfiguration,
) error {
	// Get environment from cache
	env, err := pgbackrestCredentials.EnvSetRestoreCloudCredentials(ctx,
		impl.Client,
		cluster.Namespace,
		pgbackrestConfiguration,
		pgbackrestUtils.SanitizedEnviron())
	if err != nil {
		return fmt.Errorf("can't get credentials for cluster %v: %w", cluster.Name, err)
	}
	if len(env) == 0 {
		return nil
	}

	// Instantiate the WALArchiver to get the proper configuration
	var walArchiver *pgbackrestArchiver.WALArchiver
	walArchiver, err = pgbackrestArchiver.New(
		ctx,
		env,
		impl.SpoolDirectory,
		impl.PgDataPath,
		path.Join(impl.PgDataPath, metadata.CheckEmptyWalArchiveFile))
	if err != nil {
		return fmt.Errorf("while creating the archiver: %w", err)
	}

	// TODO: refactor this code elsewhere
	stanza := cluster.Name
	for _, plugin := range cluster.Spec.Plugins {
		if plugin.IsEnabled() && plugin.Name == metadata.PluginName {
			if pluginStanza, ok := plugin.Parameters["stanza"]; ok {
				stanza = pluginStanza
			}
		}
	}

	if utils.IsEmptyWalArchiveCheckEnabled(&cluster.ObjectMeta) {
		return walArchiver.CheckWalArchiveDestination(ctx, pgbackrestConfiguration, stanza, env)
	}

	return nil
}

// restoreCustomWalDir moves the current pg_wal data to the specified custom wal dir and applies the symlink
// returns indicating if any changes were made and any error encountered in the process
func (impl JobHookImpl) restoreCustomWalDir(ctx context.Context) (bool, error) {
	const pgWalDirectory = "pg_wal"

	contextLogger := log.FromContext(ctx)
	pgDataWal := path.Join(impl.PgDataPath, pgWalDirectory)

	// if the link is already present we have nothing to do.
	if linkInfo, _ := os.Readlink(pgDataWal); linkInfo == impl.PgWalFolderToSymlink {
		contextLogger.Info("symlink to the WAL volume already present, skipping the custom wal dir restore")
		return false, nil
	}

	if err := fileutils.EnsureDirectoryExists(impl.PgWalFolderToSymlink); err != nil {
		return false, err
	}

	contextLogger.Info("restoring WAL volume symlink and transferring data")
	if err := fileutils.EnsureDirectoryExists(pgDataWal); err != nil {
		return false, err
	}

	if err := fileutils.MoveDirectoryContent(pgDataWal, impl.PgWalFolderToSymlink); err != nil {
		return false, err
	}

	if err := fileutils.RemoveFile(pgDataWal); err != nil {
		return false, err
	}

	return true, os.Symlink(impl.PgWalFolderToSymlink, pgDataWal)
}

// getRestoreWalConfig obtains the content to append to `custom.conf` allowing PostgreSQL
// to complete the WAL recovery from the object storage and then start
// as a new primary
func getRestoreWalConfig() string {
	restoreCmd := fmt.Sprintf(
		"/controller/manager wal-restore --log-destination %s/%s.json %%f %%p",
		postgres.LogPath, postgres.LogFileName)

	recoveryFileContents := fmt.Sprintf(
		"recovery_target_action = promote\n"+
			"restore_command = '%s'\n",
		restoreCmd)

	return recoveryFileContents
}

// loadBackupObjectFromExternalCluster generates an in-memory Backup structure given a reference to
// an external cluster, loading the required information from the object store
func loadBackupObjectFromExternalCluster(
	ctx context.Context,
	typedClient client.Client,
	cluster *cnpgv1.Cluster,
	recoveryArchive *pgbackrestApi.PgbackrestConfiguration,
	stanza string,
) (*cnpgv1.Backup, []string, error) {
	contextLogger := log.FromContext(ctx)

	contextLogger.Info("Recovering from external cluster",
		"stanza", stanza,
		"archive", recoveryArchive)

	env, err := pgbackrestCredentials.EnvSetRestoreCloudCredentials(
		ctx,
		typedClient,
		cluster.Namespace,
		recoveryArchive,
		pgbackrestUtils.SanitizedEnviron())
	if err != nil {
		return nil, nil, err
	}

	backupCatalog, err := pgbackrestCommand.GetBackupList(ctx, recoveryArchive, stanza, env)
	if err != nil {
		return nil, nil, err
	}

	// We are now choosing the right backup to restore
	var targetBackup *pgbackrestCatalog.PgbackrestBackup
	if cluster.Spec.Bootstrap.Recovery != nil &&
		cluster.Spec.Bootstrap.Recovery.RecoveryTarget != nil {
		targetBackup, err = backupCatalog.FindBackupInfo(
			cluster.Spec.Bootstrap.Recovery.RecoveryTarget,
		)
		if err != nil {
			return nil, nil, err
		}
	} else {
		targetBackup = backupCatalog.LatestBackupInfo()
	}
	if targetBackup == nil {
		return nil, nil, fmt.Errorf("no target backup found")
	}

	contextLogger.Info("Target backup found", "backup", targetBackup)

	return &cnpgv1.Backup{
		Spec: cnpgv1.BackupSpec{
			Cluster: cnpgv1.LocalObjectReference{
				Name: stanza,
			},
		},
		// TODO: Finish pgbackrest implementation
		Status: cnpgv1.BackupStatus{
			// BarmanCredentials: recoveryArchive.BarmanCredentials,
			// EndpointCA:        recoveryArchive.EndpointCA,
			// EndpointURL:       recoveryArchive.EndpointURL,
			// DestinationPath:   recoveryArchive.DestinationPath,
			ServerName: stanza,
			BackupID:   targetBackup.ID,
			Phase:      cnpgv1.BackupPhaseCompleted,
			StartedAt:  &metav1.Time{Time: time.Unix(targetBackup.Time.Start, 0)},
			StoppedAt:  &metav1.Time{Time: time.Unix(targetBackup.Time.Stop, 0)},
			BeginWal:   targetBackup.WAL.Start,
			EndWal:     targetBackup.WAL.Stop,
			BeginLSN:   targetBackup.LSN.Start,
			EndLSN:     targetBackup.LSN.Stop,
			// Error:         targetBackup.Error,
			CommandOutput: "",
			CommandError:  "",
			PluginMetadata: map[string]string{
				// Same value as in BackupStatus.ServerName, this field is just for
				// consistency with pgbackrest naming. ServerName comes from tthe cnpg
				// library and could be omitted but it makes experience closer to the
				// barman plugin.
				"stanza": stanza,
			},
		},
	}, env, nil
}
