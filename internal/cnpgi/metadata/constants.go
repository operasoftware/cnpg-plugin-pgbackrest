package metadata

import "github.com/cloudnative-pg/cnpg-i/pkg/identity"

// PluginName is the name of the plugin from the instance manager
// Point-of-view
const PluginName = "pgbackrest.cnpg.opera.com"

const (
	// CheckEmptyWalArchiveFile is the name of the file in the PGDATA that,
	// if present, requires the WAL archiver to check that the backup object
	// store is empty.
	CheckEmptyWalArchiveFile = ".check-empty-wal-archive"
)

// Data is the metadata of this plugin.
var Data = identity.GetPluginMetadataResponse{
	Name:          PluginName,
	Version:       "0.3.0", // x-release-please-version
	DisplayName:   "pgBackRestInstance",
	ProjectUrl:    "https://github.com/operasoftware/cnpg-plugin-pgbackrest",
	RepositoryUrl: "https://github.com/operasoftware/cnpg-plugin-pgbackrest",
	License:       "APACHE 2.0",
	LicenseUrl:    "https://github.com/operasoftware/cnpg-plugin-pgbackrest/LICENSE",
	Maturity:      "alpha",
}
