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
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/operator/config"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/catalog"
	pgbackrestCommand "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/command"
	pgbackrestCredentials "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/credentials"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/utils"
)

const (
	// defaultCatalogMaintenanceInterval mirrors the CRD default; it also paces the loop
	// when the Archive cannot be read, when a cycle fails and while maintenance is disabled.
	defaultCatalogMaintenanceInterval = 30 * time.Minute

	// backupListPageSize keeps a maintenance cycle within the small memory
	// limit of the sidecar: a namespace can hold tens of thousands of Backups.
	backupListPageSize = 500
)

// errEmptyCatalog is a sentinel because the caller logs it below the other rejections:
// an empty catalog is the normal state of a cluster whose first backup is still to come.
var errEmptyCatalog = errors.New("the catalog is empty")

// catalogUsable tells whether the catalog can decide that a backup absent from it is
// gone. Only a complete answer for the expected stanza with at least one backup can:
// an empty catalog is also what a freshly recreated stanza looks like.
func catalogUsable(c *catalog.Catalog, stanza string, repositories int) error {
	if c.Stanza != stanza {
		return fmt.Errorf("the catalog describes the stanza %q instead of %q", c.Stanza, stanza)
	}
	if !c.DescribesRepositories(repositories) {
		return fmt.Errorf(
			"the catalog does not describe all %d configured repositories (stanza status %d %q; repositories: %s)",
			repositories, c.Status.Code, c.Status.Message, describeRepoStatuses(c))
	}
	if len(c.Backups) == 0 {
		return errEmptyCatalog
	}
	return nil
}

// describeRepoStatuses renders the per-repository status codes as "key=code", so that
// the rejection names the repository that could not be read, not only that one could not.
func describeRepoStatuses(c *catalog.Catalog) string {
	statuses := make([]string, len(c.Repos))
	for idx := range c.Repos {
		statuses[idx] = fmt.Sprintf("%d=%d", c.Repos[idx].Key, c.Repos[idx].Status.Code)
	}
	return strings.Join(statuses, " ")
}

// deleteBackupsNotInCatalog deletes the completed Backup objects of the
// given cluster whose backup ID is not in the pgBackRest catalog anymore.
//
// The catalog is the source of truth: pgBackRest expires backups on its
// own after every backup, so a Backup object whose ID has disappeared
// from `pgbackrest info` points to data that no longer exists.
func deleteBackupsNotInCatalog(
	ctx context.Context,
	cli client.Client,
	cluster *cnpgv1.Cluster,
	current backupResultMetadata,
	backupIDs []string,
	catalogReadAt time.Time,
) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Debug("Checking the catalog to delete backups not present anymore")

	var errs []error
	keptElsewhere := 0
	continueToken := ""
	restarted := false
	for {
		var backups cnpgv1.BackupList
		if err := cli.List(ctx, &backups,
			client.InNamespace(cluster.GetNamespace()),
			// CloudNativePG labels every Backup it creates with its cluster; hand-written
			// Backups without the label are left alone rather than listed fleet-wide.
			client.MatchingLabels{cnpgutils.ClusterLabelName: cluster.GetName()},
			client.Limit(backupListPageSize),
			client.Continue(continueToken),
		); err != nil {
			if apierrors.IsResourceExpired(err) && !restarted {
				// The apiserver compacted the snapshot behind the token; start over once.
				// The restarted pass sees every object again, so what the aborted one
				// counted and failed to delete is re-attempted rather than reported twice.
				restarted = true
				continueToken = ""
				errs = nil
				keptElsewhere = 0
				continue
			}
			return fmt.Errorf("while getting backups: %w", err)
		}

		for idx := range backups.Items {
			backup := &backups.Items[idx]
			if !concernsCluster(backup, cluster.GetName()) {
				continue
			}

			if !useSameBackupLocation(&backup.Status, current) {
				keptElsewhere++
				continue
			}

			if slices.Contains(backupIDs, backup.Status.BackupID) {
				continue
			}

			// A backup that completed after the catalog was read cannot be in it yet.
			if backup.Status.StoppedAt != nil && backup.Status.StoppedAt.After(catalogReadAt) {
				continue
			}

			contextLogger.Info("Deleting backup not in the catalog",
				"backup", backup.Name, "backupID", backup.Status.BackupID)
			// The listed object may have been replaced by a same-named one for a live
			// backup; the precondition makes that a Conflict instead of a wrong delete.
			preconditions := client.Preconditions{UID: &backup.UID}
			if err := cli.Delete(ctx, backup, preconditions); err != nil &&
				!apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
				errs = append(errs, fmt.Errorf(
					"while deleting backup %s/%s: %w", backup.Namespace, backup.Name, err))
			}
		}

		continueToken = backups.Continue
		if continueToken == "" {
			break
		}
	}

	if keptElsewhere > 0 {
		contextLogger.Info("Kept Backup objects recorded against another location or without location metadata",
			"count", keptElsewhere)
	}

	if len(errs) > 0 {
		return fmt.Errorf("got errors while deleting Backups not in the catalog: %w", errors.Join(errs...))
	}

	return nil
}

// concernsCluster tells whether the catalog can say anything about the object at all:
// only a completed backup of this cluster carries an ID to look up in it.
func concernsCluster(backup *cnpgv1.Backup, clusterName string) bool {
	return backup.Spec.Cluster.Name == clusterName &&
		backup.Status.Phase == cnpgv1.BackupPhaseCompleted &&
		backup.Status.BackupID != ""
}

// useSameBackupLocation tells whether the backup was taken by this plugin for this
// cluster incarnation, stanza and repositories. Any entry missing from the recorded
// metadata (older plugin versions) means "unknown location" and keeps the object.
func useSameBackupLocation(backup *cnpgv1.BackupStatus, current backupResultMetadata) bool {
	if backup.Method != cnpgv1.BackupMethodPlugin {
		return false
	}

	recorded := newBackupResultMetadataFromMap(backup.PluginMetadata)
	return recorded.pluginName != "" && recorded.pluginName == current.pluginName &&
		recorded.clusterUID != "" && recorded.clusterUID == current.clusterUID &&
		recorded.stanza != "" && recorded.stanza == current.stanza &&
		recorded.repositories != "" && recorded.repositories == current.repositories
}

// CatalogMaintenanceRunnable periodically reconciles the Backup objects
// of the cluster with the pgBackRest catalog. pgBackRest expires backups
// on its own after every backup; this runnable removes the Kubernetes
// objects left behind.
type CatalogMaintenanceRunnable struct {
	Client         client.Client
	ClusterKey     types.NamespacedName
	CurrentPodName string
}

// Start runs the maintenance loop until the context is cancelled.
func (c *CatalogMaintenanceRunnable) Start(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("Starting catalog maintenance runnable")

	// Spread the fleet over the interval: without this, a rollout would fire
	// the first Backup list of every primary at the very same second.
	select {
	case <-time.After(initialDelay()):
	case <-ctx.Done():
		return nil
	}

	for {
		period, err := c.cycle(ctx)
		if err != nil {
			contextLogger.Error(err, "Catalog maintenance failed")
		}

		select {
		case <-time.After(nextInterval(period)):
		case <-ctx.Done():
			return nil
		}
	}
}

// initialDelay is how long the runnable waits before its first cycle, spreading
// the fleet uniformly over one full default interval.
func initialDelay() time.Duration {
	return rand.N(defaultCatalogMaintenanceInterval) // #nosec G404 -- spreading load, not a secret
}

// nextInterval guards against a zero or negative period, which would
// otherwise spin `pgbackrest info` against the object store in a tight loop.
func nextInterval(period time.Duration) time.Duration {
	if period <= 0 {
		return defaultCatalogMaintenanceInterval
	}
	return period
}

// cycle runs one maintenance pass and returns the interval to wait before
// the next one. A zero interval means "use the default". It runs maintenance
// on the current primary only, mirroring where backups are taken.
func (c *CatalogMaintenanceRunnable) cycle(ctx context.Context) (time.Duration, error) {
	contextLogger := log.FromContext(ctx)

	var cluster cnpgv1.Cluster
	if err := c.Client.Get(ctx, c.ClusterKey, &cluster); err != nil {
		return 0, err
	}

	enabledPlugins := cnpgv1.GetPluginConfigurationEnabledPluginNames(cluster.Spec.Plugins)
	if !slices.Contains(enabledPlugins, metadata.PluginName) {
		contextLogger.Debug("Skipping catalog maintenance: plugin is not enabled for backups")
		// The Archive is not read here, so there is no configured interval to honor.
		return defaultCatalogMaintenanceInterval, nil
	}

	if cluster.Status.CurrentPrimary != c.CurrentPodName {
		contextLogger.Debug("Skipping catalog maintenance, not the current primary",
			"currentPrimary", cluster.Status.CurrentPrimary, "podName", c.CurrentPodName)
		// The Archive is not read here, so there is no configured interval to honor.
		return defaultCatalogMaintenanceInterval, nil
	}

	configuration := config.NewFromCluster(&cluster)
	if configuration == nil || len(configuration.PgbackrestObjectName) == 0 {
		return 0, fmt.Errorf("invalid configuration, missing pgbackrestObjectName parameter")
	}

	var archive pgbackrestv1.Archive
	if err := c.Client.Get(ctx, configuration.GetArchiveObjectKey(), &archive); err != nil {
		return 0, err
	}

	interval, enabled := maintenanceInterval(&archive)
	if !enabled {
		contextLogger.Debug("Catalog maintenance is disabled for this Archive", "archive", archive.Name)
		return interval, nil
	}

	if err := c.maintenance(ctx, &cluster, &archive, configuration); err != nil {
		return 0, err
	}

	return interval, nil
}

// maintenanceInterval returns the configured interval and whether maintenance is
// enabled at all (0 disables it; nil means the CRD default).
func maintenanceInterval(archive *pgbackrestv1.Archive) (time.Duration, bool) {
	seconds := archive.Spec.InstanceSidecarConfiguration.CatalogMaintenanceIntervalSeconds
	if seconds == nil {
		return defaultCatalogMaintenanceInterval, true
	}
	if *seconds == 0 {
		return defaultCatalogMaintenanceInterval, false
	}
	return time.Duration(*seconds) * time.Second, true
}

// maintenance reads the catalog and deletes the stale Backup objects.
// cycle runs it on the current primary only.
func (c *CatalogMaintenanceRunnable) maintenance(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
	archive *pgbackrestv1.Archive,
	configuration *config.PluginConfiguration,
) error {
	contextLogger := log.FromContext(ctx)

	env, err := pgbackrestCredentials.EnvSetBackupCloudCredentials(
		ctx,
		c.Client,
		archive.Namespace,
		&archive.Spec.Configuration,
		utils.SanitizedEnviron(),
	)
	if err != nil {
		return fmt.Errorf("while setting backup cloud credentials: %w", err)
	}

	catalogReadAt := time.Now()
	backupList, err := pgbackrestCommand.GetBackupList(ctx, &archive.Spec.Configuration, configuration.Stanza, env)
	if err != nil {
		return fmt.Errorf("while reading the backup list: %w", err)
	}

	if err := catalogUsable(backupList, configuration.Stanza, len(archive.Spec.Configuration.Repositories)); err != nil {
		if errors.Is(err, errEmptyCatalog) {
			contextLogger.Info(
				"Skipping catalog maintenance: the catalog is empty — a cluster without a completed backup yet, "+
					"or a recreated stanza",
				"stanza", configuration.Stanza)
			return nil
		}
		contextLogger.Warning("Skipping catalog maintenance", "reason", err.Error(), "stanza", configuration.Stanza)
		return nil
	}

	current := newBackupResultMetadata(cluster.UID, configuration.Stanza, &archive.Spec.Configuration)
	return deleteBackupsNotInCatalog(
		ctx, c.Client, cluster, current, backupList.GetBackupIDs(), catalogReadAt)
}
