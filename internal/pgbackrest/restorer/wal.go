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
	"os"
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

	// walNotFoundExitCode is the pgbackrest archive-get exit code returned when
	// the requested WAL is not present in a valid repository.
	walNotFoundExitCode = 1
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
// specified destination path. If a prefetch is still downloading the file, it waits for it
// first, and returns ErrWALNotFound when the prefetch found the file missing.
func (restorer *WALRestorer) RestoreFromSpool(
	ctx context.Context,
	walName, destinationPath string,
) (wasInSpool bool, err error) {
	downloads.Lock()
	running, isRunning := downloads.running[restorer.spool.FileName(walName)]
	downloads.Unlock()
	if isRunning {
		select {
		case <-running.done:
			if errors.Is(running.err, ErrWALNotFound) {
				return false, running.err
			}
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}

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

// downloads tracks the WAL files that are being prefetched into a spool. A request for
// one of them waits for that download instead of starting another one, and a download
// outlives the restore_command call that started it.
var downloads = struct {
	sync.Mutex
	running map[string]*download
}{running: map[string]*download{}}

type download struct {
	done chan struct{}
	err  error
}

// RestoreWithPrefetch restores walName, the WAL file PostgreSQL requested, into
// destinationPath, and prefetches the files in prefetchList into the spool in the
// background. It returns as soon as the requested file is restored or known to be
// missing, without waiting for the prefetch. onPrefetched is called with the outcome of
// every prefetch it starts, and it returns how many it started.
func (restorer *WALRestorer) RestoreWithPrefetch(
	ctx context.Context,
	walName, destinationPath string,
	prefetchList []string,
	options []string,
	onPrefetched func(walName string, err error),
) (prefetching int, err error) {
	prefetching = restorer.prefetch(ctx, prefetchList, options, onPrefetched)
	return prefetching, restorer.restoreAndLog(ctx, walName, destinationPath, options, true)
}

// prefetch starts a background download into the spool for every file in walNames that
// is neither in the spool nor being downloaded already, keeping at most len(walNames)
// downloads running across calls.
func (restorer *WALRestorer) prefetch(
	ctx context.Context,
	walNames []string,
	options []string,
	onDone func(walName string, err error),
) (started int) {
	ctx = context.WithoutCancel(ctx)

	downloads.Lock()
	defer downloads.Unlock()
	for _, walName := range walNames {
		if len(downloads.running) >= len(walNames) {
			break
		}
		key := restorer.spool.FileName(walName)
		if _, isRunning := downloads.running[key]; isRunning {
			continue
		}
		if inSpool, err := restorer.spool.Contains(spoolName(walName)); err == nil && inSpool {
			continue
		}

		running := &download{done: make(chan struct{})}
		downloads.running[key] = running
		started++
		go func() {
			running.err = restorer.restoreIntoSpool(ctx, walName, options)
			downloads.Lock()
			delete(downloads.running, key)
			downloads.Unlock()
			close(running.done)
			if onDone != nil {
				onDone(walName, running.err)
			}
		}()
	}

	return started
}

// restoreIntoSpool downloads a WAL file into the spool. archive-get writes its destination
// in place, so the file is downloaded under a temporary name and renamed once complete:
// a file in the spool under a WAL name is always a whole one.
func (restorer *WALRestorer) restoreIntoSpool(ctx context.Context, walName string, options []string) error {
	name := spoolName(walName)
	tempFile, err := restorer.spool.CreateTemp(name)
	if err != nil {
		return err
	}
	_ = tempFile.Close()

	if err := restorer.restoreAndLog(ctx, walName, tempFile.Name(), options, false); err != nil {
		_ = os.Remove(tempFile.Name())
		return err
	}

	return os.Rename(tempFile.Name(), restorer.spool.FileName(name))
}

// spoolName is the name a prefetched WAL file gets in the spool. Partial WALs are only
// prefetched together with their full variant, which PostgreSQL requested directly, so
// the partial one is saved without its suffix and a retry restores it in place of the
// full one.
// TODO: This solution is imperfect. While partial and full WAL files should not coexist,
// it's hard to say if that can never happen. In this case there is a very small risk of
// partial file being restored when full file exists but fails to download.
func spoolName(walName string) string {
	return strings.TrimSuffix(walName, ".partial")
}

// restoreAndLog restores one WAL file and logs the outcome. Failures are logged only for
// the WAL PostgreSQL requested: the prefetch is speculative, and a prefetched WAL may just
// not exist yet.
func (restorer *WALRestorer) restoreAndLog(
	ctx context.Context,
	walName, destinationPath string,
	options []string,
	requested bool,
) error {
	contextLog := log.FromContext(ctx)
	startTime := time.Now()
	err := restorer.Restore(ctx, walName, destinationPath, options)
	endTime := time.Now()
	elapsedWalTime := endTime.Sub(startTime)

	switch {
	case err == nil:
		contextLog.Info(
			"Restored WAL file",
			"walName", walName,
			"startTime", startTime,
			"endTime", endTime,
			"elapsedWalTime", elapsedWalTime)
	case !requested:
	case errors.Is(err, ErrWALNotFound):
		contextLog.Info(
			"WAL file not found in the recovery object store",
			"walName", walName,
			"options", options,
			"startTime", startTime,
			"endTime", endTime,
			"elapsedWalTime", elapsedWalTime)
	default:
		contextLog.Warning(
			"Failed restoring WAL file (Postgres might retry)",
			"walName", walName,
			"options", options,
			"startTime", startTime,
			"endTime", endTime,
			"elapsedWalTime", elapsedWalTime,
			"error", err)
	}

	return err
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
	return walRestoreError(walName, err)
}

// walRestoreError maps a pgbackrest archive-get failure to a typed error,
// returning ErrWALNotFound when the requested WAL is missing from the archive.
func walRestoreError(walName string, err error) error {
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		return fmt.Errorf("unexpected failure retrieving %q with %s: %w",
			walName, "pgbackrest archive-get", err)
	}

	if exitError.ExitCode() == walNotFoundExitCode {
		return ErrWALNotFound
	}

	return fmt.Errorf("encountered an error: '%d' while executing %s",
		exitError.ExitCode(),
		"pgbackrest archive-get")
}
