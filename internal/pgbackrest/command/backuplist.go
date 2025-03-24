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

// Package command contains the utilities to interact with pgbackrest.
//
// This package is able to download the backup catalog, given an object store,
// and to find the required backup to recreate a cluster, given a certain point
// in time. It can also delete backups according to pgbackrest object store configuration and retention policies,
// and find the latest successful backup. This is useful to recovery from the last consistent state.
//
// A backup catalog is represented by the Catalog structure, and can be
// created using the NewCatalog function or by downloading it from an
// object store via GetBackupList. A backup catalog is just a sorted
// list of BackupInfo objects.
//
// We also have features to gather all the environment variables required
// for the pgbackrest utilities to work correctly.
//
// The functions which call the pgbackrest utilities (such as GetBackupList)
// require the environment variables to be passed, and the calling code is
// supposed gather them (i.e. via the EnvSetCloudCredentials) before calling
// them.
// A Kubernetes client is required to get the environment variables, as we
// need to download the content from the required secrets, but is not required
// to call pgbackrest.
package command

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"github.com/cloudnative-pg/machinery/pkg/log"

	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/catalog"
)

func executeQueryCommand(
	ctx context.Context,
	pgbackrestConfiguration *pgbackrestApi.PgbackrestConfiguration,
	stanza string,
	additionalOptions []string,
	env []string,
) (string, error) {
	contextLogger := log.FromContext(ctx).WithName("pgbackrest")

	options := []string{"info", "--output", "json"}

	options, err := AppendCloudProviderOptionsFromConfiguration(ctx, options, pgbackrestConfiguration)
	if err != nil {
		return "", err
	}

	options, err = AppendLogOptionsFromConfiguration(ctx, options, pgbackrestConfiguration)
	if err != nil {
		return "", err
	}

	options = append(options, "--stanza", stanza)
	options = append(options, additionalOptions...)

	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	cmd := exec.Command("pgbackrest", options...) // #nosec G204
	cmd.Env = env
	cmd.Stdout = &stdoutBuffer
	cmd.Stderr = &stderrBuffer
	err = cmd.Run()
	if err != nil {
		contextLogger.Error(err,
			"Can't extract backup list",
			"command", "pgbackrest",
			"options", options,
			"stdout", stdoutBuffer.String(),
			"stderr", stderrBuffer.String())
		return "", err
	}

	return stdoutBuffer.String(), nil
}

// GetBackupList returns the catalog reading it from the object store
func GetBackupList(
	ctx context.Context,
	pgbackrestConfiguration *pgbackrestApi.PgbackrestConfiguration,
	stanza string,
	env []string,
) (*catalog.Catalog, error) {
	contextLogger := log.FromContext(ctx).WithName("pgbackrest")

	rawJSON, err := executeQueryCommand(
		ctx,
		pgbackrestConfiguration,
		stanza,
		[]string{},
		env,
	)
	if err != nil {
		return nil, err
	}
	backupList, err := catalog.NewCatalogFromPgbackrestInfo(rawJSON)
	if err != nil {
		contextLogger.Error(err, "Can't parse pgbackrest output",
			"command", "pgbackrest info",
			"output", rawJSON)
		return nil, err
	}

	return backupList, nil
}

// GetBackupByAnnotatedName uses a name stored in an annotation to retrieve the backup.
// That's not something supported natively so all backups must be retrieved first.
func GetBackupByAnnotatedName(
	ctx context.Context,
	backupName string,
	stanza string,
	pgbackrestConfiguration *pgbackrestApi.PgbackrestConfiguration,
	env []string,
) (*catalog.Catalog, error) {
	contextLogger := log.FromContext(ctx)
	fullCatalog, err := GetBackupList(ctx, pgbackrestConfiguration, stanza, env)
	if err != nil {
		return nil, err
	}
	backupId := fullCatalog.GetBackupIdFromAnnotatedName(backupName)
	if backupId == "" {
		contextLogger.Error(err, "Can't find backup with name",
			"name", backupName)
		return nil, err
	}

	rawJSON, err := executeQueryCommand(
		ctx,
		pgbackrestConfiguration,
		stanza,
		[]string{"--set", backupId},
		env,
	)
	if err != nil {
		return nil, err
	}

	contextLogger.Debug("raw backup pgbackrest object", "rawPgbackrestObject", rawJSON)

	return catalog.NewSingleBackupCatalogFromPgbackrestInfo(rawJSON)
}

// GetLatestBackup returns the latest executed backup
func GetLatestBackup(
	ctx context.Context,
	stanza string,
	pgbackrestConfiguration *pgbackrestApi.PgbackrestConfiguration,
	env []string,
) (*catalog.PgbackrestBackup, error) {
	contextLogger := log.FromContext(ctx)
	// Extracting the latest backup using pgbackrest info
	backupList, err := GetBackupList(ctx, pgbackrestConfiguration, stanza, env)
	if err != nil {
		// Proper logging already happened inside GetBackupList
		return nil, err
	}

	contextLogger.Debug("raw backup list object", "backupList", backupList)

	// We have just made a new backup, if the backup list is empty
	// something is going wrong in the cloud storage
	if len(backupList.Backups) == 0 {
		return nil, fmt.Errorf("no backup found on the remote object storage")
	}

	return backupList.LatestBackupInfo(), nil
}
