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
	"context"
	"fmt"

	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/utils"
)

// CloudWalRestoreOptions returns the options needed to execute the pgbackrest command successfully
func CloudWalRestoreOptions(
	ctx context.Context,
	configuration *pgbackrestApi.PgbackrestConfiguration,
	clusterName string,
	pgDataDirectory string,
) ([]string, error) {
	var options []string
	options, err := AppendCloudProviderOptionsFromConfiguration(ctx, options, configuration)
	if err != nil {
		return nil, err
	}
	options, err = AppendStanzaOptionsFromConfiguration(ctx, options, configuration, pgDataDirectory, false)
	if err != nil {
		return nil, err
	}

	stanza := clusterName
	if len(configuration.Stanza) != 0 {
		stanza = configuration.Stanza
	}
	options = append(
		options,
		"--stanza",
		stanza)

	options = configuration.Wal.AppendRestoreAdditionalCommandArgs(options)

	return options, nil
}

// AppendCloudProviderOptionsFromConfiguration takes an options array and adds the cloud provider specified
// in the pgbackrest configuration object
func AppendCloudProviderOptionsFromConfiguration(
	ctx context.Context,
	options []string,
	configuration *pgbackrestApi.PgbackrestConfiguration,
) (resOptions []string, err error) {
	for index, repo := range configuration.Repositories {
		options, err = appendCloudProviderOptions(ctx, options, index, repo)
		if err != nil {
			return nil, err
		}
	}
	return options, nil
}

// AppendRetentionOptionsFromConfiguration takes an options array and adds the
// retention options specified in the pgbackrest configuration object for each repository
func AppendRetentionOptionsFromConfiguration(
	ctx context.Context,
	options []string,
	configuration *pgbackrestApi.PgbackrestConfiguration,
) (resOptions []string, err error) {
	for index, repo := range configuration.Repositories {
		options, err = appendRetentionOptions(ctx, options, index, &repo)
		if err != nil {
			return nil, err
		}
	}
	return options, nil
}

// appendRetentionOptions takes an options array and adds the retention options specified
// for the repository as arguments
func appendRetentionOptions(
	ctx context.Context,
	options []string,
	repoIndex int,
	repository *pgbackrestApi.PgbackrestRepository,
) ([]string, error) {
	if repository.Retention == nil {
		return options, nil
	}
	retention := repository.Retention

	if len(retention.ArchiveType) > 0 {
		options = append(
			options,
			utils.FormatRepoFlag(repoIndex, "retention-archive-type"),
			retention.ArchiveType)
	}
	if len(retention.FullType) > 0 {
		options = append(
			options,
			utils.FormatRepoFlag(repoIndex, "retention-full-type"),
			retention.FullType)
	}

	if retention.Archive > 0 {
		options = append(
			options,
			utils.FormatRepoFlag(repoIndex, "retention-archive"),
			fmt.Sprint(retention.Archive))
	}
	if retention.Full > 0 {
		options = append(
			options,
			utils.FormatRepoFlag(repoIndex, "retention-full"),
			fmt.Sprint(retention.Full))
	}
	if retention.Diff > 0 {
		options = append(
			options,
			utils.FormatRepoFlag(repoIndex, "retention-diff"),
			fmt.Sprint(retention.Diff))
	}

	// History retention is the only one for which 0 is a valid and meaningful value.
	// That means pointer is used to provide a distinct value when it is not set.
	if retention.History != nil {
		options = append(
			options,
			utils.FormatRepoFlag(repoIndex, "retention-history"),
			string(*retention.History))
	}

	return options, nil

}

// appendCloudProviderOptions takes an options array and adds the cloud provider specified as arguments
func appendCloudProviderOptions(
	ctx context.Context,
	options []string,
	repoIndex int,
	repository pgbackrestApi.PgbackrestRepository,
) ([]string, error) {
	options = append(
		options,
		utils.FormatRepoFlag(repoIndex, "type"),
		"s3")
	if len(repository.EndpointURL) > 0 {
		options = append(
			options,
			utils.FormatRepoFlag(repoIndex, "s3-endpoint"),
			repository.EndpointURL)
	}
	if repository.DisableVerifyTLS {
		options = append(
			options,
			utils.FormatRepoFlag(repoIndex, "storage-verify-tls=n"))
	}
	options = append(options,
		utils.FormatRepoFlag(repoIndex, "s3-bucket"), repository.Bucket,
		utils.FormatRepoFlag(repoIndex, "path"), repository.DestinationPath,
	)
	if repository.AWS != nil {
		if len(repository.AWS.UriStyle) > 0 {
			options = append(
				options,
				utils.FormatRepoFlag(repoIndex, "s3-uri-style"),
				repository.AWS.UriStyle)
		}
	}
	return options, nil
}

// AppendStanzaOptionsFromConfiguration takes an options array and adds the necessary
// stanza-specific options required for all operations connecting to the database
func AppendStanzaOptionsFromConfiguration(
	ctx context.Context,
	options []string,
	configuration *pgbackrestApi.PgbackrestConfiguration,
	pgDataDirectory string,
	clusterRunning bool,
) (resOptions []string, err error) {
	// TODO: There probably should be more entries for replicas. Is it even doable
	// to backup from a replica without SSH access or pgbackrest server running on
	// the other node?
	return appendStanzaOptions(ctx, options, 0, pgDataDirectory, clusterRunning)
}

// appendStanzaOptions takes an options array and adds the stanza-specific pgbackrest
// options required for all operations connecting to the database
func appendStanzaOptions(
	ctx context.Context,
	options []string,
	index int,
	pgDataDirectory string,
	clusterRunning bool,
) ([]string, error) {
	// TODO: Those options likely shouldn't be hardcoded.
	options = append(
		options,
		utils.FormatDbFlag(index, "path"),
		pgDataDirectory,
	)
	if clusterRunning {
		options = append(
			options,
			utils.FormatDbFlag(index, "user"),
			"postgres",
			utils.FormatDbFlag(index, "socket-path"),
			"/controller/run/",
		)
	}

	return options, nil
}

// AppendLogOptionsFromConfiguration takes an options array and adds the necessary
// stanza-specific options required for all operations connecting to the database
func AppendLogOptionsFromConfiguration(
	ctx context.Context,
	options []string,
	configuration *pgbackrestApi.PgbackrestConfiguration,
) (resOptions []string, err error) {
	return appendLogOptions(ctx, options)
}

// appendLogOptions takes an options array and adds the stanza-specific pgbackrest
// options required for all operations connecting to the database
func appendLogOptions(
	ctx context.Context,
	options []string,
) ([]string, error) {
	// TODO: Those options likely shouldn't be hardcoded.
	// TODO: Maybe configure log path to a writable directory?
	options = append(
		options,
		"--log-level-stderr",
		"warn",
		"--log-level-console",
		"off",
	)

	return options, nil
}
