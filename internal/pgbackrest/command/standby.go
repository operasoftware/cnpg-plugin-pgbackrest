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

package command

import (
	"fmt"
	"strconv"

	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/utils"
)

// Networking and filesystem defaults for the experimental "backup from
// standby" feature. These mirror pgBackRest's TLS server defaults and the
// sidecar's existing /controller layout. Certificate provisioning is
// intentionally left as a follow-up (see issue #103): the paths below are the
// contract the plugin expects the pgBackRest TLS certificates to be mounted at.
const (
	// DefaultServerPort is the port the pgBackRest TLS server listens on, and
	// the port a standby uses to reach the primary's server. It matches
	// pgBackRest's tls-server-port default.
	DefaultServerPort = 8432

	// DefaultTLSCertFile, DefaultTLSKeyFile and DefaultTLSCAFile are the mount
	// paths the plugin expects the pgBackRest TLS client/server certificate
	// material at inside every instance sidecar.
	DefaultTLSCertFile = "/controller/certificates/pgbackrest/tls.crt"
	DefaultTLSKeyFile  = "/controller/certificates/pgbackrest/tls.key"
	DefaultTLSCAFile   = "/controller/certificates/pgbackrest/ca.crt"

	// localSocketPath is the unix socket every CNPG instance pod exposes for
	// its local PostgreSQL.
	localSocketPath = "/controller/run/"

	// primaryHostIndex is the zero-based pgBackRest pg-host index used for the
	// remote primary. The local instance keeps index 0 (pg1); the primary is
	// added as pg2.
	primaryHostIndex = 1
)

// StandbyBackupTopology describes how a standby reaches the current primary's
// pgBackRest TLS server to coordinate a backup taken from the standby.
type StandbyBackupTopology struct {
	// PrimaryHost is the address (pod IP or DNS) of the current primary running
	// a pgBackRest TLS server.
	PrimaryHost string
	// PrimaryPort is the TLS server port on the primary.
	PrimaryPort int
	// PrimaryPGData is the PostgreSQL data directory on the primary. In CNPG
	// this is identical to the local instance's data directory.
	PrimaryPGData string
	// CertFile, KeyFile and CAFile are the TLS client material this instance
	// presents to the primary's pgBackRest server.
	CertFile string
	KeyFile  string
	CAFile   string
}

// ShouldConfigurePrimaryPeer reports whether a backup running on instanceName
// should coordinate with the primary. This is true only when the feature is
// enabled and this instance is a standby with a known, different primary. Whether
// a backup lands on the primary or a standby is decided by CloudNativePG via the
// backup target, not here.
func ShouldConfigurePrimaryPeer(enabled bool, currentPrimary, instanceName string) bool {
	if !enabled {
		return false
	}
	return currentPrimary != "" && currentPrimary != instanceName
}

// AppendStandbyPrimaryHostOptions appends the pgBackRest options that describe
// the remote primary (pg2-*) so that a backup running on a standby can
// coordinate pg_backup_start/stop on the primary over a TLS connection. The
// local standby remains pg1 (added by AppendStanzaOptionsFromConfiguration).
func AppendStandbyPrimaryHostOptions(options []string, topology StandbyBackupTopology) []string {
	return append(
		options,
		// how to reach the remote pgBackRest TLS server running on the primary
		utils.FormatDbFlag(primaryHostIndex, "host"), topology.PrimaryHost,
		utils.FormatDbFlag(primaryHostIndex, "host-type"), "tls",
		utils.FormatDbFlag(primaryHostIndex, "host-port"), strconv.Itoa(topology.PrimaryPort),
		utils.FormatDbFlag(primaryHostIndex, "host-cert-file"), topology.CertFile,
		utils.FormatDbFlag(primaryHostIndex, "host-key-file"), topology.KeyFile,
		utils.FormatDbFlag(primaryHostIndex, "host-ca-file"), topology.CAFile,
		// how the primary's server reaches its local PostgreSQL
		utils.FormatDbFlag(primaryHostIndex, "path"), topology.PrimaryPGData,
		utils.FormatDbFlag(primaryHostIndex, "user"), "postgres",
		utils.FormatDbFlag(primaryHostIndex, "socket-path"), localSocketPath,
	)
}

// BackupStandbyOption returns the --backup-standby flag. A backup coordinated with
// the primary always runs on a standby, so the value is always "y".
func BackupStandbyOption() string {
	return "--backup-standby=y"
}

// ServerConfig configures the pgBackRest TLS server that must run in each
// instance sidecar for backup-from-standby to work.
type ServerConfig struct {
	// Address is the interface the server binds to (pgBackRest tls-server-address).
	Address string
	// Port is the TLS server port (pgBackRest tls-server-port).
	Port int
	// PGData is the local PostgreSQL data directory.
	PGData string
	// CertFile, KeyFile and CAFile are the server's TLS material.
	CertFile string
	KeyFile  string
	CAFile   string
	// AuthClientCN is the client-certificate common name authorized to drive
	// this server, mapped to all stanzas via tls-server-auth=<cn>=*.
	AuthClientCN string
}

// PgbackrestServerOptions builds the argument list for `pgbackrest server`
// (TLS server mode). This long-running process lets a peer instance's
// pgBackRest client coordinate control commands (pg_backup_start/stop) against
// this instance's local PostgreSQL.
//
// Experimental: wiring this process into the sidecar lifecycle and providing
// its certificates is deferred pending maintainer direction (see issue #103).
func PgbackrestServerOptions(cfg ServerConfig) []string {
	options := []string{
		"server",
		"--tls-server-address", cfg.Address,
		"--tls-server-port", strconv.Itoa(cfg.Port),
		"--tls-server-cert-file", cfg.CertFile,
		"--tls-server-key-file", cfg.KeyFile,
		"--tls-server-ca-file", cfg.CAFile,
		"--tls-server-auth", fmt.Sprintf("%s=*", cfg.AuthClientCN),
		utils.FormatDbFlag(0, "path"), cfg.PGData,
		utils.FormatDbFlag(0, "user"), "postgres",
		utils.FormatDbFlag(0, "socket-path"), localSocketPath,
		"--log-level-stderr", "warn",
		"--log-level-console", "off",
	}
	return options
}
