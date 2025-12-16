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

package archiver

import (
	"context"
	"errors"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/log"

	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	pgbackrestCommand "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/command"
)

// GatherWALFilesToArchive reads from the archived status the list of WAL files
// that can be archived in parallel way.
// `requestedWALFile` is the name of the file whose archiving was requested by
// PostgreSQL, and that file is always the first of the list and is always included.
// `parallel` is the maximum number of WALs that we can archive in parallel
// It's important to ensure this method returns absolute paths. While pgbackrest can
// work with relative ones, such setup requires additional config flags and a matching
// Postgresql working directory configuration, which seems to be invalid in the context
// of the plugin's container.
func (archiver *WALArchiver) GatherWALFilesToArchive(
	ctx context.Context,
	requestedWALFile string,
	parallel int,
) (walList []string) {
	contextLog := log.FromContext(ctx)
	pgWalDirectory := path.Join(os.Getenv("PGDATA"), "pg_wal")
	archiveStatusPath := path.Join(pgWalDirectory, "archive_status")
	noMoreWALFilesNeeded := errors.New("no more files needed")

	// allocate parallel + 1 only if it does not overflow. Cap otherwise
	var walListLength int
	if parallel < math.MaxInt-1 {
		walListLength = parallel + 1
	} else {
		walListLength = math.MaxInt - 1
	}
	// slightly more optimized, but equivalent to:
	// walList = []string{requestedWALFile}
	walList = make([]string, 1, walListLength)
	// Ensure it's an absolute path. While Postgres should be configured to use absolute
	// paths in the archive command, its documentation mentions that even in this mode
	// a relative path might be used in some cases.
	walList[0] = filepath.Join(pgWalDirectory, filepath.Base(requestedWALFile))

	err := filepath.WalkDir(archiveStatusPath, func(path string, d os.DirEntry, err error) error {
		// If err is set, it means the current path is a directory and the readdir raised an error
		// The only available option here is to skip the path and log the error.
		if err != nil {
			contextLog.Error(err, "failed reading path", "path", path)
			return filepath.SkipDir
		}

		if len(walList) >= parallel {
			return noMoreWALFilesNeeded
		}

		// We don't process directories beside the archive status path
		if d.IsDir() {
			// We want to proceed exploring the archive status folder
			if path == archiveStatusPath {
				return nil
			}

			return filepath.SkipDir
		}

		// We only process ready files
		if !strings.HasSuffix(path, ".ready") {
			return nil
		}

		walFileName := strings.TrimSuffix(filepath.Base(path), ".ready")

		// We are already archiving the requested WAL file,
		// and we need to avoid archiving it twice.
		// requestedWALFile is usually "pg_wal/wal_file_name" and
		// we compare it with the path we read
		if strings.HasSuffix(requestedWALFile, walFileName) {
			return nil
		}

		walList = append(walList, filepath.Join(pgWalDirectory, walFileName))
		return nil
	})

	// In this point err must be nil or noMoreWALFilesNeeded, if it is something different
	// there is a programming error
	if err != nil && err != noMoreWALFilesNeeded {
		contextLog.Error(err, "unexpected error while reading the list of WAL files to archive")
	}

	return walList
}

// PgbackrestWalArchiveOptions calculates the set of options to be
// used with pgbackrest archive-push
func (archiver *WALArchiver) PgbackrestWalArchiveOptions(
	ctx context.Context,
	configuration *pgbackrestApi.PgbackrestConfiguration,
	clusterName string,
) (options []string, err error) {
	if len(configuration.Compression) != 0 {
		options = append(
			options,
			"--compress-type",
			string(configuration.Compression))
	}

	// TODO: Add per-repo options
	// for index, repo := range configuration.Repositories {
	//   options = append(options,)
	// }

	if configuration.Wal != nil {
		options = configuration.Wal.AppendAdditionalArchivePushCommandArgs(options)
	}

	options, err = pgbackrestCommand.AppendCloudProviderOptionsFromConfiguration(ctx, options, configuration)
	if err != nil {
		return nil, err
	}

	serverName := clusterName
	if len(configuration.Stanza) != 0 {
		serverName = configuration.Stanza
	}
	options = append(
		options,
		"--stanza",
		serverName)
	return options, nil
}
