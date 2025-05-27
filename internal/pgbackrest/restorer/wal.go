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

package restorer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/spool"
)

const (
	endOfWALStreamFlagFilename = "end-of-wal-stream"
)

// ErrWALNotFound is returned when the WAL is not found in the cloud archive
var ErrWALNotFound = errors.New("WAL not found")

// WALRestorer is a structure containing every info needed to restore
// some WALs from the object storage
type WALRestorer struct {
	// The spool of WAL files to be archived in parallel
	spool *spool.WALSpool

	// The environment that should be used to invoke pgbackrest archive-get
	env []string
}

// Result is the structure filled by the restore process on completion
type Result struct {
	// The name of the WAL file to restore
	WalName string

	// Where to store the restored WAL file
	DestinationPath string

	// If not nil, this is the error that has been detected
	Err error

	// The time when we started archive-get
	StartTime time.Time

	// The time when end archive-get ended
	EndTime time.Time
}

// NewWALRestorer creates a new WAL restorer
func NewWALRestorer(
	ctx context.Context,
	env []string,
	spoolDirectory string,
) (restorer *WALRestorer, err error) {
	contextLog := log.FromContext(ctx)
	var walRecoverSpool *spool.WALSpool

	if walRecoverSpool, err = spool.New(spoolDirectory); err != nil {
		contextLog.Info("Cannot initialize the WAL spool", "spoolDirectory", spoolDirectory)
		return nil, fmt.Errorf("while creating spool directory: %w", err)
	}

	restorer = &WALRestorer{
		spool: walRecoverSpool,
		env:   env,
	}
	return restorer, nil
}

// RestoreFromSpool restores a certain file from the spool, returning a boolean flag indicating
// is the file was in the spool or not. If the file was in the spool, it will be moved into the
// specified destination path
func (restorer *WALRestorer) RestoreFromSpool(walName, destinationPath string) (wasInSpool bool, err error) {
	err = restorer.spool.MoveOut(walName, destinationPath)
	switch {
	case err == spool.ErrorNonExistentFile:
		return false, nil

	case err != nil:
		return false, err

	default:
		return true, nil
	}
}

// SetEndOfWALStream add end-of-wal-stream in the spool directory
func (restorer *WALRestorer) SetEndOfWALStream() error {
	contains, err := restorer.IsEndOfWALStream()
	if err != nil {
		return err
	}

	if contains {
		return nil
	}

	err = restorer.spool.Touch(endOfWALStreamFlagFilename)
	if err != nil {
		return err
	}

	return nil
}

// IsEndOfWALStream check whether end-of-wal-stream flag is presents in the spool directory
func (restorer *WALRestorer) IsEndOfWALStream() (bool, error) {
	isEOS, err := restorer.spool.Contains(endOfWALStreamFlagFilename)
	if err != nil {
		return false, fmt.Errorf("failed to check end-of-wal-stream flag: %w", err)
	}

	return isEOS, nil
}

// ResetEndOfWalStream remove end-of-wal-stream flag from the spool directory
func (restorer *WALRestorer) ResetEndOfWalStream() error {
	err := restorer.spool.Remove(endOfWALStreamFlagFilename)
	if err != nil {
		return fmt.Errorf("failed to remove end-of-wal-stream flag: %w", err)
	}

	return nil
}

// RestoreList restores a list of WALs. The first WAL of the list will go directly into the
// destination path, the others will be adopted by the spool
func (restorer *WALRestorer) RestoreList(
	ctx context.Context,
	fetchList []string,
	destinationPath string,
	options []string,
) (resultList []Result) {
	resultList = make([]Result, len(fetchList))
	contextLog := log.FromContext(ctx)
	var waitGroup sync.WaitGroup

	for idx := range fetchList {
		waitGroup.Add(1)
		go func(walIndex int) {
			result := &resultList[walIndex]
			result.WalName = fetchList[walIndex]
			if walIndex == 0 {
				// The WAL that PostgreSQL requested will go directly
				// to the destination path
				result.DestinationPath = destinationPath
			} else {
				if strings.HasSuffix(result.WalName, ".partial") {
					// Partial WALs are only downloaded together with full variants.
					// Full variant is restored directly so we can safely save the
					// partial one without the suffix in spool.
					// In practice that means:
					// - try restoring full WAL directly,
					// - save partial file in spool,
					// - on retry partial file will be restored from spool in place of
					//   the full one.
					// TODO: This solution is imperfect. While partial and full WAL
					// files should not coexist, it's hard to say if that can never
					// happen. In this case there is a very small risk of partial
					// file being restored when full file exists but fails to download.
					result.DestinationPath = restorer.spool.FileName(strings.TrimSuffix(result.WalName, ".partial"))
				} else {
					result.DestinationPath = restorer.spool.FileName(result.WalName)
				}
			}

			result.StartTime = time.Now()
			result.Err = restorer.Restore(ctx, fetchList[walIndex], result.DestinationPath, options)
			result.EndTime = time.Now()

			elapsedWalTime := result.EndTime.Sub(result.StartTime)
			if result.Err == nil {
				contextLog.Info(
					"Restored WAL file",
					"walName", result.WalName,
					"startTime", result.StartTime,
					"endTime", result.EndTime,
					"elapsedWalTime", elapsedWalTime)
			} else if walIndex == 0 {
				// We don't log errors for prefetched WALs but just for the
				// first WAL, which is the one requested by PostgreSQL.
				//
				// The implemented prefetch is speculative and this WAL may just
				// not exist, this means that this may not be a real error.
				if errors.Is(result.Err, ErrWALNotFound) {
					contextLog.Info(
						"WAL file not found in the recovery object store",
						"walName", result.WalName,
						"options", options,
						"startTime", result.StartTime,
						"endTime", result.EndTime,
						"elapsedWalTime", elapsedWalTime)
				} else {
					contextLog.Warning(
						"Failed restoring WAL file (Postgres might retry)",
						"walName", result.WalName,
						"options", options,
						"startTime", result.StartTime,
						"endTime", result.EndTime,
						"elapsedWalTime", elapsedWalTime,
						"error", result.Err)
				}
			}
			waitGroup.Done()
		}(idx)
	}

	waitGroup.Wait()
	return resultList
}

// Restore restores a WAL file from the object store
func (restorer *WALRestorer) Restore(
	ctx context.Context,
	walName, destinationPath string,
	baseOptions []string,
) error {
	contextLogger := log.FromContext(ctx)

	optionsLength := len(baseOptions)
	if optionsLength >= math.MaxInt-3 {
		return fmt.Errorf("can't restore wal file %v, options too long", walName)
	}
	options := make([]string, optionsLength, optionsLength+3)
	copy(options, baseOptions)
	options = append(options, "archive-get", walName, destinationPath)

	pgbackrestWalRestoreCmd := exec.Command(
		"pgbackrest",
		options...) // #nosec G204
	pgbackrestWalRestoreCmd.Env = restorer.env

	err := execlog.RunStreaming(pgbackrestWalRestoreCmd, "pgbackrest archive-get")
	if err == nil {
		return nil
	}

	contextLogger.Error(
		err,
		"pgbackrest archive-get failed",
		"command", "pgbackrest",
		"options", options,
	)
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		return fmt.Errorf("unexpected failure retrieving %q with %s: %w",
			walName, "pgbackrest archive-get", err)
	}

	exitCode := exitError.ExitCode()

	return fmt.Errorf("encountered an error: '%d' while executing %s",
		exitCode,
		"pgbackrest archive-get")
}
