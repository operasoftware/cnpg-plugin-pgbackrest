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

package instance

import (
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"os/exec"
	"slices"
	"syscall"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/operator/config"
	pgbackrestCommand "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/command"
)

const (
	// standbyServerLocalInterval paces the checks that need no API call.
	standbyServerLocalInterval = 15 * time.Second
	// Every instance of every cluster that uses the plugin polls the API, whether the
	// feature is enabled or not; enabling it is a one-off step that can wait minutes.
	standbyServerRefreshInterval      = 5 * time.Minute
	standbyServerRefreshJitter        = 0.1
	standbyServerRefreshRetryInterval = time.Minute
	standbyServerRefreshTimeout       = 10 * time.Second
	standbyServerStopTimeout          = 10 * time.Second
)

// StandbyServerRunnable keeps a pgBackRest TLS server running in the sidecar while
// the Archive enables backups from a standby. It runs on every instance, because
// the primary changes and the standby taking the backup reaches whichever one it is.
type StandbyServerRunnable struct {
	Client     client.Reader
	ClusterKey types.NamespacedName

	// The fields below are overridable for tests only: the server command line
	// always uses the CloudNativePG certificate locations.
	CertFile            string
	KeyFile             string
	CAFile              string
	ReplicationCertFile string
	Binary              string
	StopTimeout         time.Duration

	// desired is the last state read from the API; nil means "do not run".
	desired *standbyServerConfig
	server  *standbyServer
	// certificatesError is the last reason the certificates were not usable, empty when they were.
	certificatesError string
}

// standbyServerConfig is what the API decides.
type standbyServerConfig struct {
	stanza   string
	logLevel string
}

// standbyServerIdentity is what the server authorises; changing it needs a restart.
type standbyServerIdentity struct {
	clientCN string
	stanza   string
}

type standbyServer struct {
	process     *os.Process
	exited      chan error
	identity    standbyServerIdentity
	fingerprint string
}

// Start implements the Runnable interface.
func (s *StandbyServerRunnable) Start(ctx context.Context) error {
	contextLogger := log.FromContext(ctx).WithName("standby-server")
	ctx = log.IntoContext(ctx, contextLogger)

	localTicker := time.NewTicker(standbyServerLocalInterval)
	defer localTicker.Stop()

	succeeded := s.refresh(ctx)
	s.tick(ctx)
	refreshTimer := time.NewTimer(nextRefreshDelay(succeeded, rand.Float64())) // #nosec G404
	defer refreshTimer.Stop()

	for {
		select {
		case <-refreshTimer.C:
			succeeded = s.refresh(ctx)
			s.tick(ctx)
			refreshTimer.Reset(nextRefreshDelay(succeeded, rand.Float64())) // #nosec G404
		case <-localTicker.C:
			s.tick(ctx)
		case <-ctx.Done():
			s.stop(ctx)
			return nil
		}
	}
}

// nextRefreshDelay returns the wait before the next refresh; jitter is a fraction in [0, 1)
// that spreads instances started together or failing together.
func nextRefreshDelay(succeeded bool, jitter float64) time.Duration {
	base := standbyServerRefreshRetryInterval
	if succeeded {
		base = standbyServerRefreshInterval
	}
	return base + time.Duration(jitter*standbyServerRefreshJitter*float64(base))
}

// refresh reads the desired state and reports whether the read succeeded. A missing Cluster
// or Archive disables the server; any other failure keeps the previous state, since it says
// nothing about the configuration.
func (s *StandbyServerRunnable) refresh(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, standbyServerRefreshTimeout)
	defer cancel()

	desired, err := s.desiredConfig(ctx)
	switch {
	case apierrors.IsNotFound(err):
		s.desired = nil
	case err != nil:
		log.FromContext(ctx).Error(err, "Cannot read the pgBackRest TLS server configuration, keeping the previous one")
		return false
	default:
		s.desired = desired
	}
	return true
}

// desiredConfig returns the server configuration, or nil when the server must not run.
func (s *StandbyServerRunnable) desiredConfig(ctx context.Context) (*standbyServerConfig, error) {
	var cluster cnpgv1.Cluster
	if err := s.Client.Get(ctx, s.ClusterKey, &cluster); err != nil {
		return nil, err
	}

	enabledPlugins := cnpgv1.GetPluginConfigurationEnabledPluginNames(cluster.Spec.Plugins)
	if !slices.Contains(enabledPlugins, metadata.PluginName) {
		return nil, nil
	}

	configuration := config.NewFromCluster(&cluster)
	if configuration == nil || len(configuration.PgbackrestObjectName) == 0 {
		return nil, nil
	}

	var archive pgbackrestv1.Archive
	if err := s.Client.Get(ctx, configuration.GetArchiveObjectKey(), &archive); err != nil {
		return nil, err
	}

	if !archive.Spec.Configuration.IsBackupStandbyEnabled() {
		return nil, nil
	}

	if err := pgbackrestCommand.ValidateStandbyStanza(configuration.Stanza); err != nil {
		log.FromContext(ctx).Error(err, "The pgBackRest TLS server cannot run with this stanza")
		return nil, nil
	}

	desired := &standbyServerConfig{stanza: configuration.Stanza}
	if archive.Spec.Configuration.Log != nil {
		desired.logLevel = archive.Spec.Configuration.Log.LevelStderr
	}
	return desired, nil
}

// tick brings the process in line with the desired state, without any API call.
func (s *StandbyServerRunnable) tick(ctx context.Context) {
	contextLogger := log.FromContext(ctx)

	if s.server != nil {
		select {
		case err := <-s.server.exited:
			// Restarting on a later tick keeps a server that cannot run from looping.
			s.reap(ctx, err)
			return
		default:
		}
	}

	if s.desired == nil {
		if s.server != nil {
			contextLogger.Info("Backups from a standby are disabled, stopping the pgBackRest TLS server")
			s.stop(ctx)
		}
		return
	}

	// The sidecar starts before the instance manager writes the certificates, and
	// CloudNativePG replaces a certificate and its key in two separate writes.
	snapshot, err := s.readCertificates()
	changed := s.recordCertificatesError(err)
	switch {
	case err != nil && changed:
		contextLogger.Info("The certificates of the pgBackRest TLS server are not usable", "err", err.Error())
	case err != nil:
		contextLogger.Debug("The certificates of the pgBackRest TLS server are still not usable", "err", err.Error())
	case changed:
		contextLogger.Info("The certificates of the pgBackRest TLS server are usable again")
	}
	if err != nil {
		return
	}

	// The log level is not part of the identity: a restart would terminate running backups.
	identity := standbyServerIdentity{clientCN: snapshot.clientCN, stanza: s.desired.stanza}
	if s.server != nil && s.server.identity != identity {
		contextLogger.Info("The identity of the pgBackRest TLS server changed, restarting it",
			"clientCN", identity.clientCN, "stanza", identity.stanza)
		s.stop(ctx)
	}

	if s.server == nil {
		if err := s.start(ctx, identity, s.desired.logLevel, snapshot.fingerprint); err != nil {
			contextLogger.Error(err, "Cannot start the pgBackRest TLS server")
		}
		return
	}

	if s.server.fingerprint != snapshot.fingerprint {
		contextLogger.Info("The certificates changed, reloading the pgBackRest TLS server")
		if err := s.server.process.Signal(syscall.SIGHUP); err != nil {
			contextLogger.Error(err, "Cannot reload the pgBackRest TLS server")
			return
		}
		s.server.fingerprint = snapshot.fingerprint
	}
}

// recordCertificatesError stores the outcome of reading the certificates and
// reports whether it differs from the previous one.
func (s *StandbyServerRunnable) recordCertificatesError(err error) bool {
	var text string
	if err != nil {
		text = err.Error()
	}
	changed := text != s.certificatesError
	s.certificatesError = text
	return changed
}

func (s *StandbyServerRunnable) start(
	ctx context.Context,
	identity standbyServerIdentity,
	logLevel string,
	fingerprint string,
) error {
	options := pgbackrestCommand.StandbyServerOptions(identity.clientCN, identity.stanza, logLevel)
	log.FromContext(ctx).Info("Starting the pgBackRest TLS server", "options", options)

	binary := s.Binary
	if binary == "" {
		binary = "pgbackrest"
	}
	cmd := exec.Command(binary, options...) // #nosec G204
	// A process group of its own lets us kill the per-connection children with the server.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	streaming, err := execlog.RunStreamingNoWait(cmd, "pgbackrest server")
	if err != nil {
		if cmd.Process != nil {
			killProcessGroup(ctx, cmd.Process)
			_, _ = cmd.Process.Wait()
		}
		return err
	}

	// Buffered, so that the goroutine ends even when nobody reads the result.
	exited := make(chan error, 1)
	go func() {
		exited <- streaming.Wait()
	}()

	s.server = &standbyServer{
		process:     cmd.Process,
		exited:      exited,
		identity:    identity,
		fingerprint: fingerprint,
	}
	return nil
}

// stop terminates the server and waits for it; pgBackRest forwards SIGTERM to its
// per-connection children.
func (s *StandbyServerRunnable) stop(ctx context.Context) {
	if s.server == nil {
		return
	}

	stopTimeout := s.StopTimeout
	if stopTimeout == 0 {
		stopTimeout = standbyServerStopTimeout
	}

	_ = s.server.process.Signal(syscall.SIGTERM)
	var err error
	select {
	case err = <-s.server.exited:
	case <-time.After(stopTimeout):
		log.FromContext(ctx).Info("The pgBackRest TLS server did not stop in time, killing it")
		killProcessGroup(ctx, s.server.process)
		// The wait below must end even if the group could not be signalled.
		_ = s.server.process.Kill()
		err = <-s.server.exited
	}
	s.reap(ctx, err)
}

// reap handles an exit, expected or not. Children that outlive the server, after a
// SIGKILL or a crash, must not keep serving old sessions next to a new server.
func (s *StandbyServerRunnable) reap(ctx context.Context, err error) {
	log.FromContext(ctx).Info("The pgBackRest TLS server exited", "pid", s.server.process.Pid, "err", err)
	killProcessGroup(ctx, s.server.process)
	s.server = nil
}

func killProcessGroup(ctx context.Context, process *os.Process) {
	if err := syscall.Kill(-process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		log.FromContext(ctx).Error(err, "Cannot kill the process group of the pgBackRest TLS server")
	}
}
