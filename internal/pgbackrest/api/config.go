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

// +genclient
// +kubebuilder:object:root=true
// +k8s:deepcopy-gen=package

package api

import (
	"slices"
	"strings"

	machineryapi "github.com/cloudnative-pg/machinery/pkg/api"
)

// EncryptionType encapsulated the available types of encryption
type EncryptionType string

const (
	// EncryptionTypeNone means just use the bucket configuration
	EncryptionTypeNone = EncryptionType("")

	// EncryptionTypeAES256 means to use AES256 encryption
	EncryptionTypeAES256 = EncryptionType("aes-256-cbc")
)

// CompressionType encapsulates the available types of compression
type CompressionType string

const (
	// CompressionTypeNone means no compression is performed
	CompressionTypeNone = CompressionType("")

	// CompressionTypeGzip means gzip compression is performed
	CompressionTypeGzip = CompressionType("gz")

	// CompressionTypeBzip2 means bzip2 compression is performed
	CompressionTypeBzip2 = CompressionType("bz2")

	// CompressionTypeLz4 means lz4 compression is performed
	CompressionTypeLz4 = CompressionType("lz4")

	// CompressionTypeZstd means Zstandard compression is performed
	CompressionTypeZstd = CompressionType("zst")
)

// S3Credentials is the type for the credentials to be used to upload
// files to S3. It can be provided in two alternative ways:
//
// - explicitly passing accessKeyId and secretAccessKey
//
// - inheriting the role from the pod environment by setting inheritFromIAMRole to true
type S3Credentials struct {
	// The reference to the access key ID
	// +optional
	AccessKeyIDReference *machineryapi.SecretKeySelector `json:"accessKeyId,omitempty"`

	// The reference to the secret access key
	// +optional
	SecretAccessKeyReference *machineryapi.SecretKeySelector `json:"secretAccessKey,omitempty"`

	// The reference to the secret containing the region name.
	// For S3-compatible stores like Ceph any value can be used.
	// +kubebuilder:validation:MinLength=1
	Region string `json:"region,omitempty"`

	// S3 Repository URI style, either "host" (default) or "path".
	// TODO: Enforce values via Enum like iin compression.
	// +optional
	UriStyle string `json:"uriStyle,omitempty"`
}

// PgbackrestCredentials an object containing the potential credentials for each cloud provider
type PgbackrestCredentials struct {
	// The credentials to use to upload data to S3
	// +optional
	AWS *S3Credentials `json:"s3Credentials,omitempty"`
}

// PgbackrestRetention an object containing the backup retention time for all backup
// types supported by pgbackrest.
type PgbackrestRetention struct {
	// Number of backups worth of continuous WAL to retain.
	// Can be used to aggressively expire WAL segments and save disk space.
	// However, doing so negates the ability to perform PITR from the backups with
	// expired WAL and is therefore not recommended.
	// +optional
	// +kubebuilder:validation:Maximum=9999999
	// +kubebuilder:validation:Minimum=1
	Archive int32 `json:"archive,omitempty"`

	// Backup type for WAL retention.
	// It is recommended that this setting not be changed from the default which will
	// only expire WAL in conjunction with expiring full backups.
	// Available options are `full` (default), `diff` or `incr`.
	// +optional
	// +kubebuilder:validation:Enum=full;diff;incr
	ArchiveType string `json:"archiveType,omitempty"`

	// Full backup retention count/time (in days)
	// When a full backup expires, all differential and incremental backups associated
	// with the full backup will also expire.
	// +optional
	// +kubebuilder:validation:Maximum=9999999
	// +kubebuilder:validation:Minimum=1
	Full int32 `json:"full,omitempty"`

	// Retention type for full backups.
	//  Determines whether the repo-retention-full setting represents a time period
	// (days) or count of full backups to keep.
	// Available options are `count` (default) and `time`.
	// +optional
	// +kubebuilder:validation:Enum=count;time
	FullType string `json:"fullType,omitempty"`

	// Number of differential backups to retain.
	// When a differential backup expires, all incremental backups associated with the
	// differential backup will also expire.
	// Note that full backups are included in the count of differential backups for the
	// purpose of expiration
	// +optional
	// +kubebuilder:validation:Maximum=9999999
	// +kubebuilder:validation:Minimum=1
	Diff int32 `json:"diff,omitempty"`

	// Days of backup history manifests to retain.
	// When a differential backup expires, all incremental backups associated with the
	// differential backup will also expire.
	// Note that full backups are included in the count of differential backups for the
	// purpose of expiration
	// Defaults to not set, which means those files are never removed. Set to 0 to
	// retain the backup history only for unexpired backups.
	// +optional
	// +kubebuilder:validation:Maximum=9999999
	// +kubebuilder:validation:Minimum=0
	History *int32 `json:"history,omitempty"`
}

// WalBackupConfiguration is the configuration of the backup of the
// WAL stream
type WalBackupConfiguration struct {
	// Number of WAL files to be either archived in parallel (when the
	// PostgreSQL instance is archiving to a backup object store) or
	// restored in parallel (when a PostgreSQL standby is fetching WAL
	// files from a recovery object store). If not specified, WAL files
	// will be processed one at a time. It accepts a positive integer as a
	// value - with 1 being the minimum accepted value.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxParallel int `json:"maxParallel,omitempty"`
	// Additional arguments that can be appended to the 'pgbackrest archive-push'
	// command-line invocation. These arguments provide flexibility to customize
	// the WAL archive process further, according to specific requirements or configurations.
	//
	// Example:
	// In a scenario where specialized backup options are required, such as setting
	// a specific timeout or defining custom behavior, users can use this field
	// to specify additional command arguments.
	//
	// Note:
	// It's essential to ensure that the provided arguments are valid and supported
	// by the 'pgbackrest archive-push' command, to avoid potential errors or unintended
	// behavior during execution.
	// +optional
	ArchiveAdditionalCommandArgs []string `json:"archiveAdditionalCommandArgs,omitempty"`

	// Additional arguments that can be appended to the 'pgbackrest restore'
	// command-line invocation. These arguments provide flexibility to customize
	// the WAL restore process further, according to specific requirements or configurations.
	//
	// Example:
	// In a scenario where specialized backup options are required, such as setting
	// a specific timeout or defining custom behavior, users can use this field
	// to specify additional command arguments.
	//
	// Note:
	// It's essential to ensure that the provided arguments are valid and supported
	// by the 'pgbackrest restore' command, to avoid potential errors or unintended
	// behavior during execution.
	// +optional
	RestoreAdditionalCommandArgs []string `json:"restoreAdditionalCommandArgs,omitempty"`
}

// DataBackupConfiguration is the configuration of the backup of
// the data directory
type DataBackupConfiguration struct {
	// The number of parallel jobs to be used to upload the backup, defaults
	// to 2
	// +kubebuilder:validation:Minimum=1
	// +optional
	Jobs *int32 `json:"jobs,omitempty"`

	// Control whether the I/O workload for the backup initial checkpoint will
	// be limited, according to the `checkpoint_completion_target` setting on
	// the PostgreSQL server. If set to true, an immediate checkpoint will be
	// used, meaning PostgreSQL will complete the checkpoint as soon as
	// possible. `false` by default.
	// +optional
	ImmediateCheckpoint bool `json:"immediateCheckpoint,omitempty"`

	// Annotations is a list of key value pairs that will be passed to the
	// pgbackrest --annotation option.
	// +optional
	Annotations map[string]string `json:"tags,omitempty"`

	// AdditionalCommandArgs represents additional arguments that can be appended
	// to the 'pgbackrest backup' command-line invocation. These arguments
	// provide flexibility to customize the backup process further according to
	// specific requirements or configurations.
	//
	// Example:
	// In a scenario where specialized backup options are required, such as setting
	// a specific timeout or defining custom behavior, users can use this field
	// to specify additional command arguments.
	//
	// Note:
	// It's essential to ensure that the provided arguments are valid and supported
	// by the 'pgbackrest backup' command, to avoid potential errors or unintended
	// behavior during execution.
	// +optional
	AdditionalCommandArgs []string `json:"additionalCommandArgs,omitempty"`
}

type PgbackrestRepository struct {
	// The potential credentials for each cloud provider
	PgbackrestCredentials `json:",inline"`

	// Whether to use the client-side encryption of files.
	// Allowed options are empty string (no encryption, default) and
	// `aes-256-cbc` (recommended, requires EncryptionKey defined)
	// +kubebuilder:validation:Enum="aes-256-cbc"
	// +optional
	Encryption EncryptionType `json:"encryption,omitempty"`
	// +optional
	EncryptionKey *machineryapi.SecretKeySelector `json:"encryptionKey,omitempty"`

	// Endpoint to be used to upload data to the cloud,
	// overriding the automatic endpoint discovery
	// +optional
	EndpointURL string `json:"endpointURL,omitempty"`

	// EndpointCA store the CA bundle of the pgbackrest endpoint.
	// Useful when using self-signed certificates to avoid
	// errors with certificate issuer and pgbackrest
	// +optional
	EndpointCA *machineryapi.SecretKeySelector `json:"endpointCA,omitempty"`
	// DisableVerifyTLS toggles strict certificate validation.
	// +optional
	DisableVerifyTLS bool `json:"disableVerifyTLS,omitempty"`

	// The path in the bucket where to store the backup (i.e. path/to/folder).
	// Must start with a slash character.
	// +kubebuilder:validation:Pattern=(/[a-zA-Z]*)+
	DestinationPath string `json:"destinationPath"`
	// +kubebuilder:validation:MinLength=1
	Bucket string `json:"bucket"`

	// The retention policy for backups.
	// If at least full backup retention isn't configured, both backups and WAL archives
	// will be stored in the repository indefinitely.
	// Note that automatic expiration happens only after a backup is created.
	// +optional
	Retention *PgbackrestRetention `json:"retention,omitempty"`
}
type PgbackrestConfiguration struct {
	Repositories []PgbackrestRepository `json:"repositories"`

	// The configuration for the backup of the WAL stream.
	// When not defined, WAL files will be stored uncompressed and may be
	// unencrypted in the object store, according to the bucket default policy.
	// +optional
	Wal *WalBackupConfiguration `json:"wal,omitempty"`

	// The configuration to be used to backup the data files
	// When not defined, base backups files will be stored uncompressed and may
	// be unencrypted in the object store, according to the bucket default
	// policy.
	// +optional
	Data *DataBackupConfiguration `json:"data,omitempty"`

	// Compress a WAL file before sending it to the object store. Available
	// options are empty string (no compression, default), `gz`, `bz2`, `lz4` or 'zst'.
	// +kubebuilder:validation:Enum=gz;bz2;lz4;zst
	// +optional
	Compression CompressionType `json:"compression,omitempty"`

	// Pgbackrest stanza (name used in the archive store), the cluster name is used if
	// this parameter is omitted
	// +optional
	Stanza string `json:"stanza,omitempty"`
}

// ArePopulated checks if the passed set of credentials contains
// something
func (credentials PgbackrestCredentials) ArePopulated() bool {
	return credentials.AWS != nil
}

// AppendRestoreAdditionalCommandArgs adds custom arguments as pgbackrest restore command-line options
func (cfg *WalBackupConfiguration) AppendRestoreAdditionalCommandArgs(options []string) []string {
	if cfg == nil || len(cfg.RestoreAdditionalCommandArgs) == 0 {
		return options
	}
	return appendAdditionalCommandArgs(cfg.RestoreAdditionalCommandArgs, options)
}
func appendAdditionalCommandArgs(additionalCommandArgs []string, options []string) []string {
	optionKeys := map[string]bool{}
	for _, option := range options {
		key := strings.Split(option, "=")[0]
		if key != "" {
			optionKeys[key] = true
		}
	}
	for _, additionalCommandArg := range additionalCommandArgs {
		key := strings.Split(additionalCommandArg, "=")[0]
		if key == "" || slices.Contains(options, key) || optionKeys[key] {
			continue
		}
		options = append(options, additionalCommandArg)
	}
	return options
}

// AppendAdditionalCommandArgs adds custom arguments as pgbackrest backup command-line options
func (cfg *DataBackupConfiguration) AppendAdditionalCommandArgs(options []string) []string {
	if cfg == nil || len(cfg.AdditionalCommandArgs) == 0 {
		return options
	}
	return appendAdditionalCommandArgs(cfg.AdditionalCommandArgs, options)
}

// AppendArchiveAdditionalCommandArgs adds custom arguments as pgbackrest archive-push command-line options
func (cfg *WalBackupConfiguration) AppendArchiveAdditionalCommandArgs(options []string) []string {
	if cfg == nil || len(cfg.ArchiveAdditionalCommandArgs) == 0 {
		return options
	}
	return appendAdditionalCommandArgs(cfg.ArchiveAdditionalCommandArgs, options)
}
