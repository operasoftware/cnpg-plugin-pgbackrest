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
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"

	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
)

const (
	backupResultMetadataVersionKey      = "version"
	backupResultMetadataNameKey         = "name"
	backupResultMetadataDisplayNameKey  = "displayName"
	backupResultMetadataClusterUIDKey   = "clusterUID"
	backupResultMetadataPluginNameKey   = "pluginName"
	backupResultMetadataStanzaKey       = "stanza"
	backupResultMetadataRepositoriesKey = "repositories"
)

// backupResultMetadata is the content of Backup.status.pluginMetadata written
// by this plugin: the identity keys let the catalog maintenance tell apart the
// backups taken by this plugin for this cluster, stanza and repositories from
// anything else.
type backupResultMetadata struct {
	version      string
	name         string
	displayName  string
	clusterUID   string
	pluginName   string
	stanza       string
	repositories string
}

func (b backupResultMetadata) toMap() map[string]string {
	return map[string]string{
		backupResultMetadataVersionKey:      b.version,
		backupResultMetadataNameKey:         b.name,
		backupResultMetadataDisplayNameKey:  b.displayName,
		backupResultMetadataClusterUIDKey:   b.clusterUID,
		backupResultMetadataPluginNameKey:   b.pluginName,
		backupResultMetadataStanzaKey:       b.stanza,
		backupResultMetadataRepositoriesKey: b.repositories,
	}
}

func newBackupResultMetadata(
	clusterUID types.UID,
	stanza string,
	configuration *pgbackrestApi.PgbackrestConfiguration,
) backupResultMetadata {
	return backupResultMetadata{
		clusterUID:   string(clusterUID),
		stanza:       stanza,
		repositories: describeRepositories(configuration),
		version:      metadata.Data.Version,
		name:         metadata.Data.Name,
		displayName:  metadata.Data.DisplayName,
		pluginName:   metadata.PluginName,
	}
}

// describeRepositories renders where the stanza is written, in pgBackRest's repository
// order, so that an Archive later pointed elsewhere is told apart from this one.
func describeRepositories(configuration *pgbackrestApi.PgbackrestConfiguration) string {
	locations := make([]string, len(configuration.Repositories))
	for idx := range configuration.Repositories {
		repository := &configuration.Repositories[idx]
		locations[idx] = fmt.Sprintf("%s/%s%s", repository.EndpointURL, repository.Bucket, repository.DestinationPath)
	}
	return strings.Join(locations, ",")
}

func newBackupResultMetadataFromMap(m map[string]string) backupResultMetadata {
	if m == nil {
		return backupResultMetadata{}
	}

	return backupResultMetadata{
		version:      m[backupResultMetadataVersionKey],
		name:         m[backupResultMetadataNameKey],
		displayName:  m[backupResultMetadataDisplayNameKey],
		clusterUID:   m[backupResultMetadataClusterUIDKey],
		pluginName:   m[backupResultMetadataPluginNameKey],
		stanza:       m[backupResultMetadataStanzaKey],
		repositories: m[backupResultMetadataRepositoriesKey],
	}
}
