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

// Package catalog is the implementation of a backup catalog
package catalog

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/types"
)

const (
	// LatestTimelineID is used to mark the "latest" timeline option
	LatestTimelineID = -1
	// BackupNameAnnotation is used to add CNPG backup name to the pgBackRest backup's
	// metadata as an annotation. This makes it possible to trace specific backup to
	// a Backup resource.
	BackupNameAnnotation = "cnpg-backup-name"
)

// NewCatalogFromPgbackrestInfo parses the output of pgbackrest info
func NewCatalogFromPgbackrestInfo(rawJSON string) (*Catalog, error) {
	var result []Catalog
	err := json.Unmarshal([]byte(rawJSON), &result)
	if err != nil {
		return nil, err
	}
	if len(result) != 1 {
		return nil, fmt.Errorf("expected one catalog, got %d", len(result))
	}
	return &result[0], nil
}

// LatestBackupInfo gets the information about the latest successful backup
func (catalog *Catalog) LatestBackupInfo() *PgbackrestBackup {
	if len(catalog.Backups) == 0 {
		return nil
	}

	// Skip errored backups and return the latest valid one
	for i := len(catalog.Backups) - 1; i >= 0; i-- {
		if catalog.Backups[i].isBackupDone() {
			return &catalog.Backups[i]
		}
	}

	return nil
}

// GetLastSuccessfulBackupTime gets the end time of the last successful backup or nil if no backup was successful
func (catalog *Catalog) GetLastSuccessfulBackupTime() *time.Time {
	var lastSuccessfulBackup *time.Time
	if lastSuccessfulBackupInfo := catalog.LatestBackupInfo(); lastSuccessfulBackupInfo != nil {
		stop := time.Unix(lastSuccessfulBackupInfo.Time.Stop, 0)
		return &stop
	}
	return lastSuccessfulBackup
}

// GetBackupIDs returns the list of backup IDs in the catalog
func (catalog *Catalog) GetBackupIDs() []string {
	backupIDs := make([]string, len(catalog.Backups))
	for idx, pgbackrestBackup := range catalog.Backups {
		backupIDs[idx] = pgbackrestBackup.ID
	}
	return backupIDs
}

// FirstRecoverabilityPoint gets the start time of the first backup in
// the catalog
func (catalog *Catalog) FirstRecoverabilityPoint() *time.Time {
	if len(catalog.Backups) == 0 {
		return nil
	}

	// Skip errored backups and return the first valid one
	for _, pgbackrestBackup := range catalog.Backups {
		if !pgbackrestBackup.isBackupDone() {
			continue
		}
		stop := time.Unix(pgbackrestBackup.Time.Stop, 0)
		return &stop
	}

	return nil
}

// GetFirstRecoverabilityPoint see FirstRecoverabilityPoint. This is needed to adhere to the common backup interface.
func (catalog *Catalog) GetFirstRecoverabilityPoint() *time.Time {
	return catalog.FirstRecoverabilityPoint()
}

// GetBackupMethod returns the backup method
func (catalog Catalog) GetBackupMethod() string {
	return "pgbackrest"
}

type recoveryTargetAdapter interface {
	GetBackupID() string
	GetTargetTime() string
	GetTargetLSN() string
	GetTargetTLI() string
}

// FindBackupInfo finds the backup info that should be used to file
// a PITR request via target parameters specified within `RecoveryTarget`
func (catalog *Catalog) FindBackupInfo(
	recoveryTarget recoveryTargetAdapter,
) (*PgbackrestBackup, error) {
	// TODO: Right now specific backup is used but there is no support for full PITR.
	// Maybe just let pgbackrest handle things? That would require taking restore type
	// and target values.

	// Check that BackupID is not empty. In such case, always use the
	// backup ID provided by the user.
	if recoveryTarget.GetBackupID() != "" {
		return catalog.findBackupFromID(recoveryTarget.GetBackupID())
	}

	// The user has not specified any backup ID. As a result we need
	// to automatically detect the backup from which to start the
	// recovery process.

	// Set the timeline
	// if target timeline is not an integer, it will be ignored actually despite
	// officially only "latest" being a valid string value. This matches the barman
	// plugin behavior.
	targetTimeline, err := strconv.ParseInt(recoveryTarget.GetTargetTLI(), 10, 64)
	if err != nil {
		targetTimeline = -1
	}

	// The first step is to check any time based research
	if t := recoveryTarget.GetTargetTime(); t != "" {
		return catalog.findClosestBackupFromTargetTime(t, targetTimeline)
	}

	// The second step is to check any LSN based research
	if t := recoveryTarget.GetTargetLSN(); t != "" {
		return catalog.findClosestBackupFromTargetLSN(t, targetTimeline)
	}

	// The fallback is to use the latest available backup in chronological order
	return catalog.findLatestBackupFromTimeline(targetTimeline), nil
}

func (catalog *Catalog) findClosestBackupFromTargetLSN(
	targetLSNString string,
	targetTimeline int64,
) (*PgbackrestBackup, error) {
	targetLSN := types.LSN(targetLSNString)
	if _, err := targetLSN.Parse(); err != nil {
		return nil, fmt.Errorf("while parsing recovery target targetLSN: %s", err.Error())
	}
	for i := len(catalog.Backups) - 1; i >= 0; i-- {
		pgbackrestBackup := catalog.Backups[i]
		startTimeline, err := pgbackrestBackup.startTimeline()
		if err != nil {
			continue
		}
		if !pgbackrestBackup.isBackupDone() {
			continue
		}
		if (startTimeline <= targetTimeline || targetTimeline == LatestTimelineID) &&
			types.LSN(pgbackrestBackup.LSN.Stop).Less(targetLSN) {
			return &catalog.Backups[i], nil
		}
	}
	return nil, nil
}

func (catalog *Catalog) findClosestBackupFromTargetTime(
	targetTimeString string,
	targetTimeline int64,
) (*PgbackrestBackup, error) {
	var startTimeline int64
	targetTime, err := types.ParseTargetTime(nil, targetTimeString)
	if err != nil {
		return nil, fmt.Errorf("while parsing recovery target targetTime: %s", err.Error())
	}
	for i := len(catalog.Backups) - 1; i >= 0; i-- {
		pgbackrestBackup := catalog.Backups[i]
		startTimeline, err = pgbackrestBackup.startTimeline()
		if err != nil {
			continue
		}
		if !pgbackrestBackup.isBackupDone() {
			continue
		}
		// Backups are iterated from newest to oldest, so the first backup that spans
		// the timeline is the latest one unless it has finished after the specified
		// restore time.
		if (startTimeline <= targetTimeline ||
			targetTimeline == LatestTimelineID) &&
			!time.Unix(pgbackrestBackup.Time.Stop, 0).After(targetTime) {
			return &catalog.Backups[i], nil
		}
	}
	return nil, nil
}

func (catalog *Catalog) findLatestBackupFromTimeline(targetTimeline int64) *PgbackrestBackup {
	var err error
	var startTimeline int64
	for i := len(catalog.Backups) - 1; i >= 0; i-- {
		pgbackrestBackup := catalog.Backups[i]
		startTimeline, err = pgbackrestBackup.startTimeline()
		if err != nil {
			continue
		}
		if !pgbackrestBackup.isBackupDone() {
			continue
		}
		// Backups are iterated from newest to oldest, so the first backup that spans
		// the timeline is the latest one.
		if startTimeline <= targetTimeline || targetTimeline == LatestTimelineID {
			return &catalog.Backups[i]
		}
	}

	return nil
}

func (catalog *Catalog) findBackupFromID(backupID string) (*PgbackrestBackup, error) {
	if backupID == "" {
		return nil, fmt.Errorf("no backupID provided")
	}
	for _, pgbackrestBackup := range catalog.Backups {
		if !pgbackrestBackup.isBackupDone() {
			continue
		}
		if pgbackrestBackup.ID == backupID {
			return &pgbackrestBackup, nil
		}
	}
	return nil, fmt.Errorf("no backup found with ID %s", backupID)
}

// GetBackupIDFromAnnotatedName returns the ID of the backup with the custom CNPG
// annotation set to the provided name.
func (catalog *Catalog) GetBackupIDFromAnnotatedName(backupName string) string {
	// This function is usually called to retrieve latest backup so it's more efficient
	// to iterate from the end.
	for i := len(catalog.Backups) - 1; i >= 0; i-- {
		pgbackrestBackup := catalog.Backups[i]
		if pgbackrestBackup.Annotations[BackupNameAnnotation] == backupName {
			return pgbackrestBackup.ID
		}
	}
	return ""
}

// PgbackrestBackupLSN represents an LSN range the backup contains
type PgbackrestBackupLSN struct {
	// The LSN where the backup started
	Start string `json:"start"`
	// The LSN where the backup ended
	Stop string `json:"stop"`
}

// PgbackrestBackupDatabase contains identifying metadata of the database in the stanza
type PgbackrestBackupDatabase struct {
	ID       int    `json:"id"`
	RepoKey  int    `json:"repo_key"`
	SystemID int64  `json:"system-id,omitempty"`
	Version  string `json:"version,omitempty"`
}

// PgbackrestWALArchive represents a pgBackRest WAL archive for a specific database
type PgbackrestWALArchive struct {
	ID string `json:"id"`
	// First WAL in the archive
	Min string `json:"min"`
	// Last WAL in the archive
	Max      string                   `json:"max"`
	Database PgbackrestBackupDatabase `json:"database"`
}

// PgbackrestBackupWALArchive represents a WAL archive range the backup contains
type PgbackrestBackupWALArchive struct {
	// The WAL where the backup started
	Start string `json:"start"`
	// The WAL where the backup ended
	Stop string `json:"stop"`
}

// PgbackrestBackupTime represents a time span of the creation of a single backup
type PgbackrestBackupTime struct {
	// The moment where the backup started
	Start int64 `json:"start"`

	// The moment where the backup ended
	Stop int64 `json:"stop"`
}

// PgbackrestBackup represent a backup as created by pgbackrest
type PgbackrestBackup struct {
	Annotations map[string]string `json:"annotation,omitempty"`

	Time PgbackrestBackupTime       `json:"timestamp"`
	WAL  PgbackrestBackupWALArchive `json:"archive"`
	LSN  PgbackrestBackupLSN        `json:"lsn"`

	// The ID of the backup - reusing pgbackrest's label
	ID string `json:"label"`
	// The ID of the previous backup for incremental and differential backups
	Prior string `json:"prior,omitempty"`

	// Backup type
	Type string `json:"type"`
}

// Catalog represents a catalog of archive and backup storages of a specific stanza
type Catalog struct {
	Archive    []PgbackrestWALArchive     `json:"archive"`
	Backups    []PgbackrestBackup         `json:"backup"`
	Stanza     string                     `json:"name"`
	Databases  []PgbackrestBackupDatabase `json:"db"`
	Encryption string                     `json:"cipher"`
}

// NewSingleBackupCatalogFromPgbackrestInfo parses the output of pgbackrest info
// targeting a single backup via "--set".
// While structure is the same as for the full backups list there is only a single
// backup in the list but with additional fields included.
func NewSingleBackupCatalogFromPgbackrestInfo(rawJSON string) (*Catalog, error) {
	// Currently parsing logic is exactly the same as for the full backups list.
	return NewCatalogFromPgbackrestInfo(rawJSON)
}

func (b *PgbackrestBackup) isBackupDone() bool {
	return b.Time.Start != 0 && b.Time.Stop != 0
}

func (b *PgbackrestBackup) startTimeline() (int64, error) {
	return strconv.ParseInt(b.WAL.Start[:8], 16, 0)
}

func (b *PgbackrestBackup) stopTimeline() (int64, error) { // nolint: unused
	// TODO: Is this method needed?
	return strconv.ParseInt(b.WAL.Stop[:8], 16, 0)
}
