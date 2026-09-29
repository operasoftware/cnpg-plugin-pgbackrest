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
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/archiver"
	pgbackrestBackup "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/backup"
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
			return nil, ErrMissingPermissions
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

	// Check if this WAL was already archived in a previous parallel batch
	wasInSpool, err := arch.DeleteFromSpool(request.GetSourceFileName())
	if err != nil {
		return nil, err
	}
	if wasInSpool {
		contextLogger.Info("WAL file already archived in previous parallel batch, returning immediately",
			"walName", request.GetSourceFileName())
		return &wal.WALArchiveResult{}, nil
	}

	// Check that the destination repository is reachable and its stanza exists.
	err = arch.CheckWalArchiveDestination(ctx, &archive.Spec.Configuration, configuration.Stanza, envArchive)
	switch {
	case errors.Is(err, archiver.ErrStanzaMissing):
		// On a fresh cluster, or after a major upgrade changes the repository path, the
		// stanza does not exist yet and archive-push cannot succeed until it is created.
		// When the Archive opts into it (createStanza=OnFirstArchive, the default), create
		// it here instead of waiting for the first backup: this runs on the primary as soon
		// as its sidecar is up, and PostgreSQL retries archiving on its own. stanza-create
		// is idempotent, and we only reach it when the stanza is genuinely missing, so it
		// does not contend with a running backup for the stanza lock.
		if archive.Spec.Configuration.ShouldCreateStanzaOnArchive() {
			backupCmd := pgbackrestBackup.NewBackupCommand(&archive.Spec.Configuration, nil, w.PGDataPath)
			if stanzaErr := backupCmd.CreatePgbackrestStanza(ctx, configuration.Stanza, envArchive); stanzaErr != nil {
				// Best-effort: log and continue. archive-push below reports the real
				// outcome, and PostgreSQL retries the WAL if the stanza is still missing.
				contextLogger.Warning("could not auto-create pgbackrest stanza; WAL archiving will retry",
					"stanza", configuration.Stanza, "err", stanzaErr.Error())
			} else {
				contextLogger.Info("created pgbackrest stanza so WAL archiving can start",
					"stanza", configuration.Stanza)
			}
		}
	case err != nil:
		log.Error(err, "while checking if pgbackrest repo can be used for archival")
		return nil, err
	}

	options, err := arch.PgbackrestWalArchiveOptions(ctx, &archive.Spec.Configuration, configuration.Stanza)
	if err != nil {
		return nil, err
	}

	maxParallel := 1
	if archive.Spec.Configuration.Wal != nil && archive.Spec.Configuration.Wal.MaxParallel > 1 {
		maxParallel = archive.Spec.Configuration.Wal.MaxParallel
	}

	walList := arch.GatherWALFilesToArchive(ctx, request.GetSourceFileName(), maxParallel)

	// Log the batch of WAL files prepared for archiving
	contextLogger.Info("WAL archive batch prepared",
		"requestedWalFile", request.GetSourceFileName(),
		"maxParallel", maxParallel,
		"walFiles", walList)

	result := arch.ArchiveList(ctx, walList, options)
	successfulArchives := 0
	var lastErr error
	for _, archiverResult := range result {
		if archiverResult.Err == nil {
			successfulArchives++
		} else {
			lastErr = archiverResult.Err
		}
	}

	contextLogger.Info("WAL archive batch completed",
		"requestedWalFile", request.GetSourceFileName(),
		"maxParallel", maxParallel,
		"successfulArchives", successfulArchives,
		"failedArchives", len(walList)-successfulArchives)

	if lastErr != nil {
		return nil, lastErr
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

	env, err := pgbackrestCredentials.EnvSetRestoreCloudCredentials(
		ctx,
		w.Client,
		archive.Namespace,
		&archive.Spec.Configuration,
		utils.SanitizedEnviron(),
	)
	if err != nil {
		return fmt.Errorf("while getting recover credentials: %w", err)
	}

	options, err := pgbackrestCommand.CloudWalRestoreOptions(ctx, pgbackrestConfiguration, stanza, w.PGDataPath)
	if err != nil {
		return fmt.Errorf("while getting pgbackrest archive-get options: %w", err)
	}

	// Create the restorer
	var walRestorer *pgbackrestRestorer.WALRestorer
	if walRestorer, err = pgbackrestRestorer.NewWALRestorer(ctx, env, w.SpoolDirectory); err != nil {
		return fmt.Errorf("while creating the restorer: %w", err)
	}

	// Step 1: keep the WAL files after this one downloading into the spool in the background,
	// so that they are ready when PostgreSQL asks for them. This runs on every call, also when
	// the file is in the spool, to move the prefetch forward as PostgreSQL does.
	streamingAvailable := isStreamingAvailable(cluster, w.InstanceName)
	prefetch, err := walsToPrefetch(walName, pgbackrestConfiguration.Wal, controlledPromotion)
	if err != nil {
		return fmt.Errorf("while generating the list of WAL files to restore: %w", err)
	}
	prefetch.Options = options
	prefetch.OnDone = func(prefetchedWAL string, err error) {
		// We skip this step if streaming connection is not available
		if !streamingAvailable || !errors.Is(err, pgbackrestRestorer.ErrWALNotFound) {
			return
		}
		contextLogger.Info(
			"Set end-of-wal-stream flag as one of the WAL files to be prefetched was not found",
			"walName", prefetchedWAL)
		if err := walRestorer.SetEndOfWALStream(); err != nil {
			contextLogger.Error(err, "while setting the end-of-wal-stream flag")
		}
	}
	walRestorer.Prefetch(ctx, prefetch)

	// Step 2: check if this WAL file is already in the spool, or being prefetched there
	var wasInSpool bool
	if wasInSpool, err = walRestorer.RestoreFromSpool(ctx, walName, destinationPath); err != nil {
		if errors.Is(err, pgbackrestRestorer.ErrWALNotFound) {
			return newWALNotFoundError()
		}
		return fmt.Errorf("while restoring a file from the spool directory: %w", err)
	}
	if wasInSpool {
		contextLogger.Info("Restored WAL file from spool (parallel)",
			"walName", walName,
		)
		return nil
	}

	// We skip this step if streaming connection is not available
	if streamingAvailable {
		if err := checkEndOfWALStreamFlag(walRestorer); err != nil {
			return err
		}
	}

	// Step 3: restore the requested WAL file. PostgreSQL gets its answer as soon as the file
	// is restored or known to be missing, while the prefetch goes on in the background.
	downloadStartTime := time.Now()
	prefetching, err := walRestorer.RestoreWithPrefetch(ctx, walName, destinationPath, prefetch)
	if err != nil {
		// The failure has already been logged in walRestorer.RestoreWithPrefetch method
		if errors.Is(err, pgbackrestRestorer.ErrWALNotFound) {
			return newWALNotFoundError()
		}

		return err
	}

	contextLogger.Info("WAL restore command completed (parallel)",
		"walName", walName,
		"maxParallel", prefetch.Parallel,
		"prefetching", prefetching,
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
			return nil, ErrMissingPermissions
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

	// First instance of a new cluster: streaming is not available yet
	if cluster.Status.CurrentPrimary == "" {
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

// walsToPrefetch returns what to prefetch into the spool for a request of walName: the next
// maxPrefetch WAL files, maxParallel - 1 when it is not set, downloaded up to maxParallel at
// a time. With maxParallel 1, or for a file that is not a regular WAL, nothing is prefetched
// but the partial WAL of a controlled promotion.
func walsToPrefetch(
	walName string,
	configuration *pgbackrestApi.WalBackupConfiguration,
	controlledPromotion bool,
) (pgbackrestRestorer.Prefetch, error) {
	prefetch := pgbackrestRestorer.Prefetch{Parallel: 1}
	lookahead := 0
	if configuration != nil && configuration.MaxParallel > 1 {
		prefetch.Parallel = configuration.MaxParallel
		lookahead = configuration.MaxParallel - 1
		if configuration.MaxPrefetch > 0 {
			lookahead = configuration.MaxPrefetch
		}
	}
	if !IsWALFile(walName) {
		return prefetch, nil
	}

	walList, err := gatherWALFilesToRestore(walName, lookahead+1, controlledPromotion)
	if err != nil {
		return prefetch, err
	}
	prefetch.WALs = walList[1:]
	return prefetch, nil
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
