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
	prefetches.Lock()
	var running *download
	if state, ok := prefetches.bySpool[restorer.spool.Directory()]; ok {
		running = state.running[walName]
	}
	prefetches.Unlock()
	if running != nil {
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

// Prefetch describes the WAL files a restore_command call wants downloaded into the spool
// ahead of PostgreSQL.
type Prefetch struct {
	// WALs are the files to have ready in the spool, nearest first.
	WALs []string

	// Parallel is the number of downloads that may run at a time, counting the one for
	// the file PostgreSQL waits for, so Parallel - 1 are left for the prefetch. When
	// Parallel is 1 a single prefetch still runs, for the partial file of a promotion.
	Parallel int

	// Options are the pgbackrest archive-get options.
	Options []string

	// OnDone, when set, is called with the outcome of every prefetch.
	OnDone func(walName string, err error)
}

// prefetches keeps, for each spool directory, the WAL files being downloaded into it and
// the files the latest restore_command call wants there. Downloads outlive the call that
// started them, and each one that completes starts the next wanted file, so a slow
// download does not leave the others idle until PostgreSQL asks for another file.
var prefetches = struct {
	sync.Mutex
	bySpool map[string]*prefetchState
}{bySpool: map[string]*prefetchState{}}

type prefetchState struct {
	restorer *WALRestorer
	ctx      context.Context
	wanted   Prefetch
	running  map[string]*download

	// failed is the first wanted WAL file whose prefetch failed. Nothing from it on is
	// prefetched until PostgreSQL asks for it, so a file that is missing or cannot be
	// downloaded is not retried in a loop.
	failed string
}

type download struct {
	done chan struct{}
	err  error
}

// RestoreWithPrefetch restores walName, the WAL file PostgreSQL requested and did not
// find in the spool, into destinationPath, and prefetches p in the background. It returns
// as soon as the requested file is restored or known to be missing, without waiting for
// the prefetch, together with the number of prefetches it started. Once PostgreSQL asks
// for a file whose prefetch failed, the prefetch goes past it again.
func (restorer *WALRestorer) RestoreWithPrefetch(
	ctx context.Context,
	walName, destinationPath string,
	p Prefetch,
) (prefetching int, err error) {
	prefetches.Lock()
	if state := restorer.prefetchState(ctx); state.failed != "" && walName >= state.failed {
		state.failed = ""
	}
	prefetches.Unlock()

	prefetching = restorer.Prefetch(ctx, p)
	return prefetching, restorer.restoreAndLog(ctx, walName, destinationPath, p.Options, true)
}

// Prefetch replaces the files wanted in the spool with p.WALs and starts downloading
// them, nearest first, skipping the ones already in the spool or being downloaded. It
// returns how many downloads it started.
func (restorer *WALRestorer) Prefetch(ctx context.Context, p Prefetch) (started int) {
	prefetches.Lock()
	defer prefetches.Unlock()

	state := restorer.prefetchState(ctx)
	state.restorer, state.ctx, state.wanted = restorer, context.WithoutCancel(ctx), p
	return state.fill()
}

// prefetchState returns the prefetch state of the restorer's spool. The first call for a
// spool removes the temporary files that downloads interrupted by a restart of the
// sidecar left there. Must be called with prefetches locked.
func (restorer *WALRestorer) prefetchState(ctx context.Context) *prefetchState {
	directory := restorer.spool.Directory()
	state, ok := prefetches.bySpool[directory]
	if !ok {
		if err := restorer.spool.RemoveTemp(); err != nil {
			log.FromContext(ctx).Warning("Cannot remove temporary files from the spool", "err", err)
		}
		state = &prefetchState{restorer: restorer, running: map[string]*download{}}
		prefetches.bySpool[directory] = state
	}
	return state
}

// fill starts downloads for the wanted files until the prefetch limit is reached,
// stopping at the file whose prefetch failed. Must be called with prefetches locked.
func (state *prefetchState) fill() (started int) {
	limit := max(state.wanted.Parallel-1, 1)
	for _, walName := range state.wanted.WALs {
		if len(state.running) >= limit || (state.failed != "" && walName >= state.failed) {
			break
		}
		if _, isRunning := state.running[walName]; isRunning {
			continue
		}
		if inSpool, err := state.restorer.spool.Contains(spoolName(walName)); err == nil && inSpool {
			continue
		}
		state.start(walName)
		started++
	}
	return started
}

// start downloads walName into the spool in the background. When the download ends, it
// refills the prefetch from the files wanted at that point. Must be called with
// prefetches locked.
func (state *prefetchState) start(walName string) {
	restorer, ctx, p := state.restorer, state.ctx, state.wanted
	running := &download{done: make(chan struct{})}
	state.running[walName] = running

	go func() {
		running.err = restorer.restoreIntoSpool(ctx, walName, p.Options)

		prefetches.Lock()
		delete(state.running, walName)
		if running.err != nil && (state.failed == "" || walName < state.failed) {
			state.failed = walName
		}
		state.fill()
		prefetches.Unlock()

		close(running.done)
		if p.OnDone != nil {
			p.OnDone(walName, running.err)
		}
	}()
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
