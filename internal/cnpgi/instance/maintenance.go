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
	"slices"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/machinery/pkg/log"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/catalog"
)

// Keys of the entries this plugin writes in the "pluginMetadata" field of the
// Backup resources it takes
const (
	versionMetadataKey      = "version"
	pluginNameMetadataKey   = "name"
	displayNameMetadataKey  = "displayName"
	clusterUIDMetadataKey   = "clusterUID"
	stanzaMetadataKey       = "stanza"
	repositoriesMetadataKey = "repositories"
)

// backupResultMetadata is what this plugin records in the "pluginMetadata" field of
// the Backup resources it takes. Besides naming the plugin, it pins down where the
// data was written, so that a resource can later be told apart from one describing
// data that lives somewhere else.
type backupResultMetadata struct {
	pluginName  string
	version     string
	displayName string

	// clusterUID is the UID of the cluster the backup was taken from. Backup
	// resources are not owned by the cluster, so they outlive it: this tells them
	// apart from the ones of a cluster later recreated under the same name.
	clusterUID string

	// stanza is the pgBackRest stanza the backup belongs to
	stanza string

	// repositories is where the stanza was written, so that an Archive pointed at
	// a different bucket is not mistaken for the one holding the older backups
	repositories string
}

// newBackupResultMetadata builds the metadata to record for a backup taken from the
// given cluster and stanza into the repositories of the given configuration
func newBackupResultMetadata(
	cluster *cnpgv1.Cluster,
	stanza string,
	configuration *pgbackrestApi.PgbackrestConfiguration,
) backupResultMetadata {
	return backupResultMetadata{
		pluginName:   metadata.Data.Name,
		version:      metadata.Data.Version,
		displayName:  metadata.Data.DisplayName,
		clusterUID:   string(cluster.UID),
		stanza:       stanza,
		repositories: describeRepositories(configuration),
	}
}

// describeRepositories renders the location of every repository of the given
// configuration, in the order pgBackRest indexes them
func describeRepositories(configuration *pgbackrestApi.PgbackrestConfiguration) string {
	locations := make([]string, len(configuration.Repositories))
	for idx := range configuration.Repositories {
		repository := &configuration.Repositories[idx]
		locations[idx] = fmt.Sprintf(
			"%s/%s%s",
			repository.EndpointURL,
			repository.Bucket,
			repository.DestinationPath,
		)
	}

	return strings.Join(locations, ",")
}

// toMap renders the metadata the way the operator stores it in the Backup status
func (m backupResultMetadata) toMap() map[string]string {
	return map[string]string{
		versionMetadataKey:      m.version,
		pluginNameMetadataKey:   m.pluginName,
		displayNameMetadataKey:  m.displayName,
		clusterUIDMetadataKey:   m.clusterUID,
		stanzaMetadataKey:       m.stanza,
		repositoriesMetadataKey: m.repositories,
	}
}

// newBackupResultMetadataFromMap reads back the metadata a Backup resource recorded
func newBackupResultMetadataFromMap(recorded map[string]string) backupResultMetadata {
	return backupResultMetadata{
		pluginName:   recorded[pluginNameMetadataKey],
		version:      recorded[versionMetadataKey],
		displayName:  recorded[displayNameMetadataKey],
		clusterUID:   recorded[clusterUIDMetadataKey],
		stanza:       recorded[stanzaMetadataKey],
		repositories: recorded[repositoriesMetadataKey],
	}
}

// describesSameLocation tells whether the recorded metadata belongs to a backup this
// plugin took and wrote where m says it writes.
//
// The entries a resource does not carry are not compared: they were written by a
// version of the plugin that did not record them yet, and there is nothing to compare
// them against. Such a resource is attributed to the current location, which is the
// only one it can be checked against: a cluster whose stanza or repositories were
// changed while running an older version of the plugin therefore loses the resources
// of the backups it took before the change.
func (m backupResultMetadata) describesSameLocation(recorded backupResultMetadata) bool {
	if recorded.pluginName != m.pluginName {
		return false
	}

	return matchesIfRecorded(recorded.clusterUID, m.clusterUID) &&
		matchesIfRecorded(recorded.stanza, m.stanza) &&
		matchesIfRecorded(recorded.repositories, m.repositories)
}

// matchesIfRecorded compares the two entries unless the recorded one is absent
func matchesIfRecorded(recorded string, current string) bool {
	return len(recorded) == 0 || recorded == current
}

// isComplete tells whether the metadata pins down a location. Comparing resources
// against an entry we do not have ourselves would attribute every one of them to this
// location, which is what recording it is meant to prevent.
func (m backupResultMetadata) isComplete() bool {
	return len(m.pluginName) > 0 &&
		len(m.clusterUID) > 0 &&
		len(m.stanza) > 0 &&
		len(m.repositories) > 0
}

// reconcileBackupsWithCatalog deletes the Backup resources whose data is not in the
// pgBackRest repositories anymore.
//
// The retention policy is enforced at the end of every base backup, dropping the
// expired backups from the repositories. Nothing removes the Backup resources that
// describe them: CloudNativePG garbage collects the resources of the backups it takes
// itself, with the in-tree barmanObjectStore method, but not the ones taken through a
// plugin. Without this step the Backup resources of a cluster grow without bound, and
// keep advertising restore points whose data has already been deleted.
func reconcileBackupsWithCatalog(
	ctx context.Context,
	cli client.Client,
	cluster *cnpgv1.Cluster,
	resultMetadata backupResultMetadata,
	backupCatalog *catalog.Catalog,
	repositories int,
) error {
	contextLogger := log.FromContext(ctx)

	if !resultMetadata.isComplete() {
		return fmt.Errorf(
			"incomplete backup location: plugin %q, cluster UID %q, stanza %q, repositories %q",
			resultMetadata.pluginName,
			resultMetadata.clusterUID,
			resultMetadata.stanza,
			resultMetadata.repositories,
		)
	}

	if backupCatalog.Stanza != resultMetadata.stanza {
		return fmt.Errorf(
			"the catalog describes the stanza %q instead of %q",
			backupCatalog.Stanza,
			resultMetadata.stanza,
		)
	}

	// "pgbackrest info" does not fail when one repository of the stanza cannot be
	// read: it reports that repository as failed and returns what the others hold.
	// Acting on such an answer would delete the resources of every backup that only
	// exists in the repository we could not reach.
	if !backupCatalog.DescribesRepositories(repositories) {
		contextLogger.Warning(
			"Incomplete backup catalog, skipping the Backup resources reconciliation",
			"stanza", resultMetadata.stanza,
			"configuredRepositories", repositories,
			"catalogStatus", backupCatalog.Status,
			"catalogRepositories", backupCatalog.Repos)
		return nil
	}

	// An empty catalog is not a trustworthy description of the repositories: it is
	// also what a freshly recreated stanza looks like. This runs right after a backup
	// has been taken, so the catalog is expected to hold at least that one.
	if len(backupCatalog.Backups) == 0 {
		contextLogger.Warning(
			"Empty backup catalog, skipping the Backup resources reconciliation",
			"stanza", resultMetadata.stanza)
		return nil
	}

	return deleteBackupsNotInCatalog(ctx, cli, cluster, resultMetadata, backupCatalog.GetBackupIDs())
}

// deleteBackupsNotInCatalog deletes all the Backup resources of the given cluster
// that this plugin wrote where the given metadata says, and whose backup is not in
// the passed list of IDs anymore
func deleteBackupsNotInCatalog(
	ctx context.Context,
	cli client.Client,
	cluster *cnpgv1.Cluster,
	resultMetadata backupResultMetadata,
	backupIDs []string,
) error {
	contextLogger := log.FromContext(ctx)

	var backups cnpgv1.BackupList
	if err := cli.List(ctx, &backups, client.InNamespace(cluster.Namespace)); err != nil {
		return fmt.Errorf("while listing the backups of the cluster: %w", err)
	}

	var errs []error
	for idx := range backups.Items {
		backupResource := &backups.Items[idx]

		if !isExpiredBackupResource(backupResource, cluster, resultMetadata, backupIDs) {
			continue
		}

		contextLogger.Info("Deleting the Backup resource of an expired backup",
			"backupResourceName", backupResource.Name,
			"backupID", backupResource.Status.BackupID,
			"stanza", resultMetadata.stanza)

		// The resource we delete has to be the one we read: between the list and
		// this call a resource with the same name may have been recreated for a
		// backup that is very much alive.
		// A conflict means the precondition did not hold, so the resource we listed is
		// already gone and another one holds its name: nothing left for us to do.
		preconditions := client.Preconditions{UID: &backupResource.UID}
		err := cli.Delete(ctx, backupResource, preconditions)
		if err != nil && !apierrs.IsNotFound(err) && !apierrs.IsConflict(err) {
			errs = append(errs, fmt.Errorf(
				"while deleting backup %s/%s: %w",
				backupResource.Namespace,
				backupResource.Name,
				err,
			))
		}
	}

	return errors.Join(errs...)
}

// isExpiredBackupResource tells whether the given Backup resource describes a backup
// that this plugin took for this cluster, into the repositories the passed backup IDs
// were read from, and whose data has been expired from them
func isExpiredBackupResource(
	backupResource *cnpgv1.Backup,
	cluster *cnpgv1.Cluster,
	resultMetadata backupResultMetadata,
	backupIDs []string,
) bool {
	if backupResource.Spec.Cluster.Name != cluster.Name {
		return false
	}

	recorded := newBackupResultMetadataFromMap(backupResource.Status.PluginMetadata)
	if !resultMetadata.describesSameLocation(recorded) {
		return false
	}

	// Only a completed backup has data in the repositories to be expired. Anything
	// else is either still running or a record of a failure, and both are none of our
	// business.
	if backupResource.Status.Phase != cnpgv1.BackupPhaseCompleted {
		return false
	}

	// Without a backup ID there is nothing to look for in the catalog, so we cannot
	// tell whether the data is still there.
	if len(backupResource.Status.BackupID) == 0 {
		return false
	}

	return !slices.Contains(backupIDs, backupResource.Status.BackupID)
}
