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

package common

import (
	"context"
	"errors"
	"fmt"
	"path"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cnpg-i/pkg/wal"
	"github.com/cloudnative-pg/machinery/pkg/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/operator/config"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/archiver"
	pgbackrestCommand "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/command"
	pgbackrestCredentials "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/credentials"
	pgbackrestRestorer "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/restorer"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/utils"
)

// WALServiceImplementation is the implementation of the WAL Service
type WALServiceImplementation struct {
	wal.UnimplementedWALServer
	Client         client.Client
	InstanceName   string
	SpoolDirectory string
	PGDataPath     string
	PGWALPath      string
}

// GetCapabilities implements the WALService interface
func (w WALServiceImplementation) GetCapabilities(
	_ context.Context,
	_ *wal.WALCapabilitiesRequest,
) (*wal.WALCapabilitiesResult, error) {
	return &wal.WALCapabilitiesResult{
		Capabilities: []*wal.WALCapability{
			{
				Type: &wal.WALCapability_Rpc{
					Rpc: &wal.WALCapability_RPC{
						Type: wal.WALCapability_RPC_TYPE_ARCHIVE_WAL,
					},
				},
			},
			{
				Type: &wal.WALCapability_Rpc{
					Rpc: &wal.WALCapability_RPC{
						Type: wal.WALCapability_RPC_TYPE_RESTORE_WAL,
					},
				},
			},
			{
				Type: &wal.WALCapability_Rpc{
					Rpc: &wal.WALCapability_RPC{
						Type: wal.WALCapability_RPC_TYPE_STATUS,
					},
				},
			},
		},
	}, nil
}

// Archive implements the WALService interface
func (w WALServiceImplementation) Archive(
	ctx context.Context,
	request *wal.WALArchiveRequest,
) (*wal.WALArchiveResult, error) {
	contextLogger := log.FromContext(ctx)
	contextLogger.Debug("starting wal archive")

	configuration, err := config.NewFromClusterJSON(request.ClusterDefinition)
	if err != nil {
		return nil, err
	}

	var archive pgbackrestv1.Archive
	if err := w.Client.Get(ctx, configuration.GetArchiveObjectKey(), &archive); err != nil {
		return nil, err
	}

	envArchive, err := pgbackrestCredentials.EnvSetBackupCloudCredentials(
		ctx,
		w.Client,
		archive.Namespace,
		&archive.Spec.Configuration,
		utils.SanitizedEnviron())
	if err != nil {
		if apierrors.IsForbidden(err) {
			return nil, errors.New("backup credentials don't yet have access permissions. Will retry reconciliation loop")
		}
		return nil, err
	}

	arch, err := archiver.New(
		ctx,
		envArchive,
		w.SpoolDirectory,
		w.PGDataPath,
		path.Join(w.PGDataPath, metadata.CheckEmptyWalArchiveFile),
	)
	if err != nil {
		return nil, err
	}

	// Check if we're ok to archive in the desired destination
	err = arch.CheckWalArchiveDestination(ctx, &archive.Spec.Configuration, configuration.Stanza, envArchive)
	if err != nil {
		log.Error(err, "while checking if pgbackrest repo can be used for archival")
		return nil, err
	}

	options, err := arch.PgbackrestWalArchiveOptions(ctx, &archive.Spec.Configuration, configuration.Stanza)
	if err != nil {
		return nil, err
	}
	walList := arch.GatherWALFilesToArchive(ctx, request.GetSourceFileName(), 1)
	result := arch.ArchiveList(ctx, walList, options)
	for _, archiverResult := range result {
		if archiverResult.Err != nil {
			return nil, archiverResult.Err
		}
	}

	return &wal.WALArchiveResult{}, nil
}

// Restore implements the WALService interface
// nolint: gocognit
func (w WALServiceImplementation) Restore(
	ctx context.Context,
	request *wal.WALRestoreRequest,
) (*wal.WALRestoreResult, error) {
	contextLogger := log.FromContext(ctx)

	walName := request.GetSourceWalName()
	destinationPath := request.GetDestinationFileName()

	configuration, err := config.NewFromClusterJSON(request.ClusterDefinition)
	if err != nil {
		return nil, err
	}

	var stanza string
	var archiveKey types.NamespacedName
	controlledPromotion := false

	var promotionToken string
	if configuration.Cluster.Spec.ReplicaCluster != nil {
		promotionToken = configuration.Cluster.Spec.ReplicaCluster.PromotionToken
	}

	switch {
	case promotionToken != "" && configuration.Cluster.Status.LastPromotionToken != promotionToken:
		// This is a replica cluster that is being promoted to a primary cluster
		// Recover from the replica source archive
		stanza = configuration.ReplicaSourceStanza
		archiveKey = configuration.GetReplicaSourceArchiveObjectKey()
		controlledPromotion = true

	case configuration.Cluster.IsReplica() && configuration.Cluster.Status.CurrentPrimary == w.InstanceName:
		// Designated primary on the replica cluster, using the replica source archive
		stanza = configuration.ReplicaSourceStanza
		archiveKey = configuration.GetReplicaSourceArchiveObjectKey()

	case configuration.Cluster.Status.CurrentPrimary == "":
		// Recovery from an archive, using recovery archive
		stanza = configuration.RecoveryStanza
		archiveKey = configuration.GetRecoveryArchiveObjectKey()

	default:
		// Using the cluster archive
		stanza = configuration.Stanza
		archiveKey = configuration.GetArchiveObjectKey()
	}

	var archive pgbackrestv1.Archive
	if err := w.Client.Get(ctx, archiveKey, &archive); err != nil {
		return nil, err
	}

	contextLogger.Info(
		"Restoring WAL file",
		"archive", archive.Name,
		"stanza", stanza,
		"walName", walName)
	return &wal.WALRestoreResult{}, w.restoreFromPgbackrestArchive(
		ctx, configuration.Cluster, &archive, stanza, walName, destinationPath, controlledPromotion)
}

func (w WALServiceImplementation) restoreFromPgbackrestArchive(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
	archive *pgbackrestv1.Archive,
	stanza string,
	walName string,
	destinationPath string,
	controlledPromotion bool,
) error {
	contextLogger := log.FromContext(ctx)
	startTime := time.Now()

	pgbackrestConfiguration := &archive.Spec.Configuration

	env := GetRestoreCABundleEnv(pgbackrestConfiguration)
	credentialsEnv, err := pgbackrestCredentials.EnvSetBackupCloudCredentials(
		ctx,
		w.Client,
		archive.Namespace,
		&archive.Spec.Configuration,
		utils.SanitizedEnviron(),
	)
	if err != nil {
		return fmt.Errorf("while getting recover credentials: %w", err)
	}
	env = MergeEnv(env, credentialsEnv)

	options, err := pgbackrestCommand.CloudWalRestoreOptions(ctx, pgbackrestConfiguration, stanza, w.PGDataPath)
	if err != nil {
		return fmt.Errorf("while getting pgbackrest archive-get options: %w", err)
	}

	// Create the restorer
	var walRestorer *pgbackrestRestorer.WALRestorer
	if walRestorer, err = pgbackrestRestorer.NewWALRestorer(ctx, env, w.SpoolDirectory); err != nil {
		return fmt.Errorf("while creating the restorer: %w", err)
	}

	// Step 1: check if this WAL file is not already in the spool
	var wasInSpool bool
	if wasInSpool, err = walRestorer.RestoreFromSpool(walName, destinationPath); err != nil {
		return fmt.Errorf("while restoring a file from the spool directory: %w", err)
	}
	if wasInSpool {
		contextLogger.Info("Restored WAL file from spool (parallel)",
			"walName", walName,
		)
		return nil
	}

	// We skip this step if streaming connection is not available
	if isStreamingAvailable(cluster, w.InstanceName) {
		if err := checkEndOfWALStreamFlag(walRestorer); err != nil {
			return err
		}
	}

	// Step 3: gather the WAL files names to restore. If the required file isn't a regular WAL, we download it directly.
	var walFilesList []string
	maxParallel := 1
	if pgbackrestConfiguration.Wal != nil && pgbackrestConfiguration.Wal.MaxParallel > 1 {
		maxParallel = pgbackrestConfiguration.Wal.MaxParallel
	}
	if IsWALFile(walName) {
		// If this is a regular WAL file, we try to prefetch
		if walFilesList, err = gatherWALFilesToRestore(walName, maxParallel, controlledPromotion); err != nil {
			return fmt.Errorf("while generating the list of WAL files to restore: %w", err)
		}
	} else {
		// This is not a regular WAL file, we fetch it directly
		walFilesList = []string{walName}
	}

	// Step 4: download the WAL files into the required place
	downloadStartTime := time.Now()
	walStatus := walRestorer.RestoreList(ctx, walFilesList, destinationPath, options)

	// We return immediately if the first WAL has errors, because the first WAL
	// is the one that PostgreSQL has requested to restore.
	// The failure has already been logged in walRestorer.RestoreList method
	if walStatus[0].Err != nil {
		if errors.Is(walStatus[0].Err, pgbackrestRestorer.ErrWALNotFound) {
			return newWALNotFoundError()
		}

		return walStatus[0].Err
	}

	// We skip this step if streaming connection is not available
	endOfWALStream := isEndOfWALStream(walStatus)
	if isStreamingAvailable(cluster, w.InstanceName) && endOfWALStream {
		contextLogger.Info(
			"Set end-of-wal-stream flag as one of the WAL files to be prefetched was not found")

		err = walRestorer.SetEndOfWALStream()
		if err != nil {
			return err
		}
	}

	successfulWalRestore := 0
	for idx := range walStatus {
		if walStatus[idx].Err == nil {
			successfulWalRestore++
		}
	}

	contextLogger.Info("WAL restore command completed (parallel)",
		"walName", walName,
		"maxParallel", maxParallel,
		"successfulWalRestore", successfulWalRestore,
		"failedWalRestore", maxParallel-successfulWalRestore,
		"startTime", startTime,
		"downloadStartTime", downloadStartTime,
		"downloadTotalTime", time.Since(downloadStartTime),
		"totalTime", time.Since(startTime))

	return nil
}

// Status implements the WALService interface
func (w WALServiceImplementation) Status(
	ctx context.Context,
	request *wal.WALStatusRequest,
) (*wal.WALStatusResult, error) {
	contextLogger := log.FromContext(ctx)
	contextLogger.Debug("checking archive status")

	configuration, err := config.NewFromClusterJSON(request.ClusterDefinition)
	if err != nil {
		return nil, err
	}

	var archive pgbackrestv1.Archive
	if err := w.Client.Get(ctx, configuration.GetArchiveObjectKey(), &archive); err != nil {
		return nil, err
	}

	env, err := pgbackrestCredentials.EnvSetBackupCloudCredentials(
		ctx,
		w.Client,
		archive.Namespace,
		&archive.Spec.Configuration,
		utils.SanitizedEnviron())
	if err != nil {
		if apierrors.IsForbidden(err) {
			return nil, errors.New("backup credentials don't yet have access permissions. Will retry reconciliation loop")
		}
		return nil, err
	}

	backupCatalog, err := pgbackrestCommand.GetBackupList(ctx, &archive.Spec.Configuration, configuration.Stanza, env)
	if err != nil {
		return nil, err
	}

	if len(backupCatalog.Archive) == 0 {
		return nil, errors.New("no WAL files found in the archive")
	}

	result := wal.WALStatusResult{
		FirstWal: backupCatalog.Archive[0].Min,
		LastWal:  backupCatalog.Archive[0].Max,
	}

	return &result, nil
}

// SetFirstRequired implements the WALService interface
func (w WALServiceImplementation) SetFirstRequired(
	_ context.Context,
	_ *wal.SetFirstRequiredRequest,
) (*wal.SetFirstRequiredResult, error) {
	// TODO implement me
	panic("implement me")
}

// isStreamingAvailable checks if this pod can replicate via streaming connection.
func isStreamingAvailable(cluster *cnpgv1.Cluster, podName string) bool {
	if cluster == nil {
		return false
	}

	// Easy case: If this pod is a replica, the streaming is always available
	if cluster.Status.CurrentPrimary != podName {
		return true
	}

	// Designated primary in a replica cluster: return true if the external cluster has streaming connection
	if cluster.IsReplica() {
		externalCluster, found := cluster.ExternalCluster(cluster.Spec.ReplicaCluster.Source)

		// This is a configuration error
		if !found {
			return false
		}

		return externalCluster.ConnectionParameters != nil
	}

	// Primary, we do not replicate from nobody
	return false
}

// gatherWALFilesToRestore files a list of possible WAL files to restore, always
// including as the first one the requested WAL file.
func gatherWALFilesToRestore(walName string, parallel int, controlledPromotion bool) (walList []string, err error) {
	var segment Segment

	segment, err = SegmentFromName(walName)
	if err != nil {
		// This seems an invalid segment name. It's not a problem
		// because PostgreSQL may request also other files such as
		// backup, history, etc.
		// Let's just avoid prefetching in this case
		return []string{walName}, nil
	}
	// NextSegments would accept postgresVersion and segmentSize,
	// but we do not have this info here, so we pass nil.
	segmentList := segment.NextSegments(parallel, nil, nil)
	walList = make([]string, len(segmentList))
	for idx := range segmentList {
		walList[idx] = segmentList[idx].Name()
	}
	// TODO: Consider explicitly downloading the partial file when full file not found.
	// That would avoid breaking the parallel limit for parallel==1.
	if controlledPromotion && (len(segmentList) < parallel || parallel == 1) {
		// Last WAL file during a token-based promotion can be (always is?) a partial one
		// and pgbackrest won't download it unless extension is explicitly included.
		// nolint: makezero // This is a rare operation that most likely should be rewritten anyway.
		walList = append(walList, walList[len(walList)-1]+".partial")
	}

	return walList, err
}

// ErrEndOfWALStreamReached is returned when end of WAL is detected in the cloud archive.
var ErrEndOfWALStreamReached = errors.New("end of WAL reached")

// checkEndOfWALStreamFlag returns ErrEndOfWALStreamReached if the flag is set in the restorer.
func checkEndOfWALStreamFlag(walRestorer *pgbackrestRestorer.WALRestorer) error {
	contain, err := walRestorer.IsEndOfWALStream()
	if err != nil {
		return err
	}

	if contain {
		err := walRestorer.ResetEndOfWalStream()
		if err != nil {
			return err
		}

		return ErrEndOfWALStreamReached
	}
	return nil
}

// isEndOfWALStream returns true if one of the downloads has returned
// a file-not-found error.
func isEndOfWALStream(results []pgbackrestRestorer.Result) bool {
	for _, result := range results {
		if errors.Is(result.Err, pgbackrestRestorer.ErrWALNotFound) {
			return true
		}
	}

	return false
}
