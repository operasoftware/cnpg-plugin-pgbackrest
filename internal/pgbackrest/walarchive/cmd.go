/*
Copyright The CloudNativePG Contributors

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

package walarchive

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"sync"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
)

const ArchiveCommand = "archive-push"
const PgbackrestExecutable = "pgbackrest"

// PgbackrestArchiver implements a WAL archiver based
// on pgbackrest
type PgbackrestArchiver struct {
	Env                 []string
	Touch               func(walFile string) error
	EmptyWalArchivePath string
}

// WALArchiverResult contains the result of the archival of one WAL
type WALArchiverResult struct {
	// The WAL that have been archived
	WalName string

	// If not nil, this is the error that has been detected
	Err error

	// The time when we started pgbackrest archive-push
	StartTime time.Time

	// The time when pgbackrest archive-push ended
	EndTime time.Time
}

// Archive archives a certain WAL file using pgbackrest archive-push.
// See archiveWALFileList for the meaning of the parameters
func (archiver *PgbackrestArchiver) Archive(
	ctx context.Context,
	walName string,
	baseOptions []string,
) error {
	contextLogger := log.FromContext(ctx)
	optionsLength := len(baseOptions)
	if optionsLength >= math.MaxInt-2 {
		return fmt.Errorf("can't archive wal file %v, options too long", walName)
	}
	options := make([]string, optionsLength, optionsLength+2)
	copy(options, baseOptions)
	options = append(options, ArchiveCommand, walName)

	contextLogger.Info("Executing pgbackrest "+ArchiveCommand,
		"walName", walName,
		"options", options,
	)

	pgbackrestWalArchiveCmd := exec.Command(PgbackrestExecutable, options...) // #nosec G204
	pgbackrestWalArchiveCmd.Env = archiver.Env

	err := execlog.RunStreaming(pgbackrestWalArchiveCmd, ArchiveCommand)
	if err != nil {
		contextLogger.Error(err, "Error invoking "+ArchiveCommand,
			"walName", walName,
			"options", options,
			"exitCode", pgbackrestWalArchiveCmd.ProcessState.ExitCode(),
		)
		return fmt.Errorf("unexpected failure invoking %s: %w", ArchiveCommand, err)
	}

	// Removes the `.check-empty-wal-archive` file inside PGDATA after the
	// first successful archival of a WAL file.
	if err := fileutils.RemoveFile(archiver.EmptyWalArchivePath); err != nil {
		return fmt.Errorf("error while deleting the check WAL file flag: %w", err)
	}
	return nil
}

// ArchiveList archives a list of WAL files in parallel
func (archiver *PgbackrestArchiver) ArchiveList(
	ctx context.Context,
	walNames []string,
	options []string,
) (result []WALArchiverResult) {
	contextLog := log.FromContext(ctx)
	result = make([]WALArchiverResult, len(walNames))

	var waitGroup sync.WaitGroup
	for idx := range walNames {
		waitGroup.Add(1)
		go func(walIndex int) {
			walStatus := &result[walIndex]
			walStatus.WalName = walNames[walIndex]
			walStatus.StartTime = time.Now()
			walStatus.Err = archiver.Archive(ctx, walNames[walIndex], options)
			walStatus.EndTime = time.Now()
			if walStatus.Err == nil && walIndex != 0 {
				walStatus.Err = archiver.Touch(walNames[walIndex])
			}

			elapsedWalTime := walStatus.EndTime.Sub(walStatus.StartTime)
			if walStatus.Err != nil {
				contextLog.Warning(
					"Failed archiving WAL: PostgreSQL will retry",
					"walName", walStatus.WalName,
					"startTime", walStatus.StartTime,
					"endTime", walStatus.EndTime,
					"elapsedWalTime", elapsedWalTime,
					"error", walStatus.Err)
			} else {
				contextLog.Info(
					"Archived WAL file",
					"walName", walStatus.WalName,
					"startTime", walStatus.StartTime,
					"endTime", walStatus.EndTime,
					"elapsedWalTime", elapsedWalTime)
			}

			waitGroup.Done()
		}(idx)
	}

	waitGroup.Wait()
	return result
}
