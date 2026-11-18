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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
)

const standbyArchiveName = "archive-example"

var standbyClusterKey = types.NamespacedName{Namespace: testNamespace, Name: "cluster-example"}

func newStandbyCluster(parameters map[string]string) *cnpgv1.Cluster {
	cluster := newTestCluster()
	cluster.Spec.Plugins = []cnpgv1.PluginConfiguration{{Name: metadata.PluginName, Parameters: parameters}}
	return cluster
}

func newStandbyArchive(standby *pgbackrestApi.BackupStandbyConfiguration) *pgbackrestv1.Archive {
	return &pgbackrestv1.Archive{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: standbyArchiveName},
		Spec: pgbackrestv1.ArchiveSpec{
			Configuration: pgbackrestApi.PgbackrestConfiguration{BackupStandby: standby},
		},
	}
}

type testKeyPair struct {
	cert []byte
	key  []byte
}

func newTestKeyPair(commonName string) testKeyPair {
	GinkgoHelper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).ToNot(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).ToNot(HaveOccurred())
	keyDER, err := x509.MarshalECPrivateKey(key)
	Expect(err).ToNot(HaveOccurred())
	return testKeyPair{
		cert: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		key:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	}
}

func writeTestFile(file string, data []byte) {
	GinkgoHelper()
	Expect(os.WriteFile(file, data, 0o600)).To(Succeed())
}

// newCertificateRunnable returns a runnable whose certificates are a consistent set in dir
func newCertificateRunnable(dir string) *StandbyServerRunnable {
	GinkgoHelper()
	runnable := &StandbyServerRunnable{
		CertFile:            filepath.Join(dir, "server.crt"),
		KeyFile:             filepath.Join(dir, "server.key"),
		CAFile:              filepath.Join(dir, "client-ca.crt"),
		ReplicationCertFile: filepath.Join(dir, "streaming_replica.crt"),
	}
	server := newTestKeyPair("cluster-example-rw")
	writeTestFile(runnable.CertFile, server.cert)
	writeTestFile(runnable.KeyFile, server.key)
	writeTestFile(runnable.CAFile, newTestKeyPair("cluster-example").cert)
	writeTestFile(runnable.ReplicationCertFile, newTestKeyPair(cnpgv1.StreamingReplicationUser).cert)
	return runnable
}

var _ = Describe("StandbyServerRunnable desiredConfig", func() {
	enabledParameters := map[string]string{"pgbackrestObjectName": standbyArchiveName}

	runnableFor := func(objects ...client.Object) *StandbyServerRunnable {
		return &StandbyServerRunnable{
			Client:     fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build(),
			ClusterKey: standbyClusterKey,
		}
	}

	It("returns nil when the plugin is not enabled", func(ctx SpecContext) {
		cluster := newStandbyCluster(enabledParameters)
		cluster.Spec.Plugins[0].Enabled = ptr.To(false)
		archive := newStandbyArchive(&pgbackrestApi.BackupStandbyConfiguration{Enabled: true})

		desired, err := runnableFor(cluster, archive).desiredConfig(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(desired).To(BeNil())
	})

	It("returns nil when the plugin has no Archive name", func(ctx SpecContext) {
		desired, err := runnableFor(newStandbyCluster(nil)).desiredConfig(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(desired).To(BeNil())
	})

	It("returns nil when the Archive has no backupStandby", func(ctx SpecContext) {
		desired, err := runnableFor(newStandbyCluster(enabledParameters), newStandbyArchive(nil)).desiredConfig(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(desired).To(BeNil())
	})

	It("returns nil when backupStandby is not enabled", func(ctx SpecContext) {
		archive := newStandbyArchive(&pgbackrestApi.BackupStandbyConfiguration{Enabled: false})
		desired, err := runnableFor(newStandbyCluster(enabledParameters), archive).desiredConfig(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(desired).To(BeNil())
	})

	It("uses the cluster name as the default stanza", func(ctx SpecContext) {
		archive := newStandbyArchive(&pgbackrestApi.BackupStandbyConfiguration{Enabled: true})
		desired, err := runnableFor(newStandbyCluster(enabledParameters), archive).desiredConfig(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(desired).To(Equal(&standbyServerConfig{stanza: "cluster-example"}))
	})

	It("uses the stanza and the log level of the configuration", func(ctx SpecContext) {
		cluster := newStandbyCluster(map[string]string{"pgbackrestObjectName": standbyArchiveName, "stanza": "custom"})
		archive := newStandbyArchive(&pgbackrestApi.BackupStandbyConfiguration{Enabled: true})
		archive.Spec.Configuration.Log = &pgbackrestApi.LogConfiguration{LevelStderr: "info"}

		desired, err := runnableFor(cluster, archive).desiredConfig(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(desired).To(Equal(&standbyServerConfig{stanza: "custom", logLevel: "info"}))
	})

	DescribeTable("returns nil when the stanza cannot be authorised",
		func(ctx SpecContext, stanza string) {
			cluster := newStandbyCluster(map[string]string{"pgbackrestObjectName": standbyArchiveName, "stanza": stanza})
			archive := newStandbyArchive(&pgbackrestApi.BackupStandbyConfiguration{Enabled: true})

			desired, err := runnableFor(cluster, archive).desiredConfig(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(desired).To(BeNil())
		},
		Entry("every stanza", "*"),
		Entry("a list of stanzas", "a,b"),
	)

	It("turns the server off when the Archive does not exist", func(ctx SpecContext) {
		runnable := runnableFor(newStandbyCluster(enabledParameters))
		runnable.desired = &standbyServerConfig{stanza: "cluster-example"}
		runnable.refresh(ctx)
		Expect(runnable.desired).To(BeNil())
	})

	It("turns the server off when the Cluster does not exist", func(ctx SpecContext) {
		runnable := runnableFor()
		runnable.desired = &standbyServerConfig{stanza: "cluster-example"}
		runnable.refresh(ctx)
		Expect(runnable.desired).To(BeNil())
	})

	It("keeps the previous desired state when the API cannot be reached", func(ctx SpecContext) {
		runnable := runnableFor()
		runnable.Client = fake.NewClientBuilder().WithScheme(scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
					return errors.New("connection refused")
				},
			}).Build()
		runnable.desired = &standbyServerConfig{stanza: "cluster-example"}
		runnable.refresh(ctx)
		Expect(runnable.desired).To(Equal(&standbyServerConfig{stanza: "cluster-example"}))
	})
})

var _ = Describe("StandbyServerRunnable refresh", func() {
	var (
		cli             client.Client
		runnable        *StandbyServerRunnable
		clusterGets     int
		archiveGets     int
		clusterGetError error
	)

	BeforeEach(func() {
		clusterGets, archiveGets, clusterGetError = 0, 0, nil
		cli = fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(
				newStandbyCluster(map[string]string{"pgbackrestObjectName": standbyArchiveName}),
				newStandbyArchive(&pgbackrestApi.BackupStandbyConfiguration{Enabled: true}),
			).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(
					ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object,
					opts ...client.GetOption,
				) error {
					switch obj.(type) {
					case *cnpgv1.Cluster:
						clusterGets++
						if clusterGetError != nil {
							return clusterGetError
						}
					case *pgbackrestv1.Archive:
						archiveGets++
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}).Build()
		runnable = &StandbyServerRunnable{Client: cli, ClusterKey: standbyClusterKey}
	})

	It("reads the Cluster and the Archive on every refresh", func(ctx SpecContext) {
		Expect(runnable.refresh(ctx)).To(BeTrue())
		Expect(clusterGets).To(Equal(1))
		Expect(archiveGets).To(Equal(1))
		Expect(runnable.desired).To(Equal(&standbyServerConfig{stanza: "cluster-example"}))

		Expect(runnable.refresh(ctx)).To(BeTrue())
		Expect(clusterGets).To(Equal(2))
		Expect(archiveGets).To(Equal(2))
	})

	It("does not read the Archive while the plugin is not enabled", func(ctx SpecContext) {
		var cluster cnpgv1.Cluster
		Expect(cli.Get(ctx, standbyClusterKey, &cluster)).To(Succeed())
		cluster.Spec.Plugins[0].Enabled = ptr.To(false)
		Expect(cli.Update(ctx, &cluster)).To(Succeed())
		clusterGets = 0

		Expect(runnable.refresh(ctx)).To(BeTrue())
		Expect(runnable.refresh(ctx)).To(BeTrue())
		Expect(runnable.desired).To(BeNil())
		Expect(clusterGets).To(Equal(2))
		Expect(archiveGets).To(BeZero())
	})

	It("turns the server off when the Archive does not exist", func(ctx SpecContext) {
		Expect(runnable.refresh(ctx)).To(BeTrue())
		Expect(runnable.desired).ToNot(BeNil())

		Expect(cli.Delete(ctx, newStandbyArchive(nil))).To(Succeed())
		Expect(runnable.refresh(ctx)).To(BeTrue())
		Expect(runnable.desired).To(BeNil())
	})

	It("uses the replacement Archive after pgbackrestObjectName changes", func(ctx SpecContext) {
		var first pgbackrestv1.Archive
		Expect(cli.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: standbyArchiveName}, &first)).To(Succeed())
		first.Spec.Configuration.BackupStandby.Enabled = false
		Expect(cli.Update(ctx, &first)).To(Succeed())
		Expect(runnable.refresh(ctx)).To(BeTrue())
		Expect(runnable.desired).To(BeNil())

		second := newStandbyArchive(&pgbackrestApi.BackupStandbyConfiguration{Enabled: true})
		second.Name = "archive-replacement"
		Expect(cli.Create(ctx, second)).To(Succeed())
		var cluster cnpgv1.Cluster
		Expect(cli.Get(ctx, standbyClusterKey, &cluster)).To(Succeed())
		cluster.Spec.Plugins[0].Parameters["pgbackrestObjectName"] = second.Name
		Expect(cli.Update(ctx, &cluster)).To(Succeed())

		Expect(runnable.refresh(ctx)).To(BeTrue())
		Expect(runnable.desired).To(Equal(&standbyServerConfig{stanza: "cluster-example"}))
	})

	It("keeps the desired state and skips the Archive when the Cluster cannot be read", func(ctx SpecContext) {
		Expect(runnable.refresh(ctx)).To(BeTrue())
		desired := runnable.desired
		Expect(desired).ToNot(BeNil())

		clusterGetError = errors.New("connection refused")
		Expect(runnable.refresh(ctx)).To(BeFalse())
		Expect(runnable.desired).To(BeIdenticalTo(desired))
		Expect(clusterGets).To(Equal(2))
		Expect(archiveGets).To(Equal(1))

		clusterGetError = nil
		Expect(runnable.refresh(ctx)).To(BeTrue())
		Expect(archiveGets).To(Equal(2))
	})
})

var _ = Describe("nextRefreshDelay", func() {
	It("waits five minutes plus up to 10% after a successful refresh", func() {
		Expect(nextRefreshDelay(true, 0)).To(Equal(5 * time.Minute))
		Expect(nextRefreshDelay(true, 0.5)).To(Equal(5*time.Minute + 15*time.Second))
		Expect(nextRefreshDelay(true, 0.9999)).To(BeNumerically("<", 5*time.Minute+30*time.Second))
		Expect(nextRefreshDelay(true, 0.9999)).To(BeNumerically(">", 5*time.Minute+29*time.Second))
	})

	It("retries after a minute plus up to 10% after a failed refresh", func() {
		Expect(nextRefreshDelay(false, 0)).To(Equal(time.Minute))
		Expect(nextRefreshDelay(false, 0.5)).To(Equal(time.Minute + 3*time.Second))
		Expect(nextRefreshDelay(false, 0.9999)).To(BeNumerically("<", time.Minute+6*time.Second))
		Expect(nextRefreshDelay(false, 0.9999)).To(BeNumerically(">", time.Minute+5*time.Second))
	})
})

var _ = Describe("StandbyServerRunnable readCertificates", func() {
	var runnable *StandbyServerRunnable

	BeforeEach(func() {
		runnable = newCertificateRunnable(GinkgoT().TempDir())
	})

	It("returns the common name of the replication certificate", func() {
		snapshot, err := runnable.readCertificates()
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshot.clientCN).To(Equal(cnpgv1.StreamingReplicationUser))
	})

	It("is stable while the files are untouched", func() {
		first, err := runnable.readCertificates()
		Expect(err).ToNot(HaveOccurred())
		second, err := runnable.readCertificates()
		Expect(err).ToNot(HaveOccurred())
		Expect(second).To(Equal(first))
	})

	It("changes when the client CA or the replication certificate is replaced", func() {
		first, err := runnable.readCertificates()
		Expect(err).ToNot(HaveOccurred())

		writeTestFile(runnable.CAFile, newTestKeyPair("cluster-example").cert)
		second, err := runnable.readCertificates()
		Expect(err).ToNot(HaveOccurred())
		Expect(second.fingerprint).ToNot(Equal(first.fingerprint))

		writeTestFile(runnable.ReplicationCertFile, newTestKeyPair(cnpgv1.StreamingReplicationUser).cert)
		third, err := runnable.readCertificates()
		Expect(err).ToNot(HaveOccurred())
		Expect(third.fingerprint).ToNot(Equal(second.fingerprint))
	})

	It("fails when a file is missing", func() {
		Expect(os.Remove(runnable.ReplicationCertFile)).To(Succeed())
		_, err := runnable.readCertificates()
		Expect(err).To(HaveOccurred())
	})

	It("fails when the key does not match the certificate", func() {
		writeTestFile(runnable.CertFile, newTestKeyPair("cluster-example-rw").cert)
		_, err := runnable.readCertificates()
		Expect(err).To(MatchError(ContainSubstring("server certificate and key")))
	})

	It("fails when the client CA holds no certificate", func() {
		writeTestFile(runnable.CAFile, []byte("not a certificate"))
		_, err := runnable.readCertificates()
		Expect(err).To(MatchError(ContainSubstring("no certificate found")))
	})

	It("fails when the replication certificate has no common name", func() {
		writeTestFile(runnable.ReplicationCertFile, newTestKeyPair("").cert)
		_, err := runnable.readCertificates()
		Expect(err).To(MatchError(ContainSubstring("empty common name")))
	})

	It("fails when the common name of the replication certificate contains =", func() {
		writeTestFile(runnable.ReplicationCertFile, newTestKeyPair("a=b").cert)
		_, err := runnable.readCertificates()
		Expect(err).To(MatchError(ContainSubstring(`contains "="`)))
	})
})

var _ = Describe("StandbyServerRunnable recordCertificatesError", func() {
	It("reports the first error, a different error and the recovery, not a repeat", func() {
		runnable := &StandbyServerRunnable{}
		Expect(runnable.recordCertificatesError(nil)).To(BeFalse())
		Expect(runnable.recordCertificatesError(errors.New("missing key"))).To(BeTrue())
		Expect(runnable.recordCertificatesError(errors.New("missing key"))).To(BeFalse())
		Expect(runnable.recordCertificatesError(errors.New("key mismatch"))).To(BeTrue())
		Expect(runnable.certificatesError).To(Equal("key mismatch"))
		Expect(runnable.recordCertificatesError(nil)).To(BeTrue())
		Expect(runnable.certificatesError).To(BeEmpty())
		Expect(runnable.recordCertificatesError(nil)).To(BeFalse())
	})
})

// standInStart is a start of the stand-in server, as recorded in its events file
type standInStart struct {
	pid   int
	child int
	args  string
}

// processRunning treats a zombie as gone: an orphan killed under a PID 1 that
// does not reap stays a zombie.
func processRunning(pid int) bool {
	if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		return false
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	return err != nil || !strings.Contains(string(stat), ") Z ")
}

var _ = Describe("StandbyServerRunnable supervision", func() {
	const (
		processTimeout = 5 * time.Second
		processPolling = 10 * time.Millisecond
	)

	var (
		dir       string
		events    string
		runnable  *StandbyServerRunnable
		cli       client.Client
		failGets  bool
		blockGets bool
	)

	// writeStandIn writes a stand-in for pgbackrest that records its starts and
	// reloads, and keeps a child that outlives it, like a per-connection process
	writeStandIn := func(onTerm string) {
		script := fmt.Sprintf(`#!/bin/sh
trap 'echo "hup $$" >> "%[1]s"' HUP
trap '%[2]s' TERM
sleep 1000 &
child=$!
echo "start $$ $child $*" >> "%[1]s"
while kill -0 "$child" 2>/dev/null; do wait "$child"; done
`, events, onTerm)
		Expect(os.WriteFile(runnable.Binary, []byte(script), 0o700)).To(Succeed()) // #nosec G306
	}

	readEvents := func() []string {
		data, err := os.ReadFile(events) // #nosec G304
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		Expect(err).ToNot(HaveOccurred())
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}

	starts := func() []standInStart {
		var result []standInStart
		for _, line := range readEvents() {
			fields := strings.SplitN(line, " ", 4)
			if fields[0] != "start" {
				continue
			}
			pid, err := strconv.Atoi(fields[1])
			Expect(err).ToNot(HaveOccurred())
			child, err := strconv.Atoi(fields[2])
			Expect(err).ToNot(HaveOccurred())
			result = append(result, standInStart{pid: pid, child: child, args: fields[3]})
		}
		return result
	}

	reloads := func(pid int) func() int {
		return func() int {
			return strings.Count(strings.Join(readEvents(), "\n")+"\n", fmt.Sprintf("hup %d\n", pid))
		}
	}

	// running waits until the current server has installed its signal handlers
	running := func() standInStart {
		GinkgoHelper()
		Expect(runnable.server).ToNot(BeNil())
		pid := runnable.server.process.Pid
		var found standInStart
		Eventually(func() bool {
			for _, start := range starts() {
				if start.pid == pid {
					found = start
					return true
				}
			}
			return false
		}).WithTimeout(processTimeout).WithPolling(processPolling).Should(BeTrue())
		return found
	}

	expectGone := func(start standInStart) {
		GinkgoHelper()
		Eventually(processRunning).WithArguments(start.pid).
			WithTimeout(processTimeout).WithPolling(processPolling).Should(BeFalse())
		Eventually(processRunning).WithArguments(start.child).
			WithTimeout(processTimeout).WithPolling(processPolling).Should(BeFalse())
	}

	updateArchive := func(ctx context.Context, change func(*pgbackrestv1.Archive)) {
		GinkgoHelper()
		var archive pgbackrestv1.Archive
		Expect(cli.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: standbyArchiveName}, &archive)).
			To(Succeed())
		change(&archive)
		Expect(cli.Update(ctx, &archive)).To(Succeed())
	}

	BeforeEach(func(ctx SpecContext) {
		dir = GinkgoT().TempDir()
		events = filepath.Join(dir, "events")
		failGets = false
		blockGets = false
		cli = fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(
				newStandbyCluster(map[string]string{"pgbackrestObjectName": standbyArchiveName}),
				newStandbyArchive(&pgbackrestApi.BackupStandbyConfiguration{Enabled: true}),
			).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(
					ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object,
					opts ...client.GetOption,
				) error {
					if failGets {
						return errors.New("connection refused")
					}
					if blockGets {
						<-ctx.Done()
						return ctx.Err()
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}).Build()

		runnable = newCertificateRunnable(dir)
		runnable.Client = cli
		runnable.ClusterKey = standbyClusterKey
		runnable.Binary = filepath.Join(dir, "pgbackrest")
		runnable.StopTimeout = 200 * time.Millisecond
		writeStandIn("exit 0")
		DeferCleanup(func() {
			runnable.stop(context.Background())
		})

		runnable.refresh(ctx)
		Expect(runnable.desired).ToNot(BeNil())
	})

	It("does not start while the key does not match the certificate", func(ctx SpecContext) {
		rotated := newTestKeyPair("cluster-example-rw")
		writeTestFile(runnable.CertFile, rotated.cert)
		runnable.tick(ctx)
		Expect(runnable.server).To(BeNil())

		writeTestFile(runnable.KeyFile, rotated.key)
		runnable.tick(ctx)
		Expect(running().args).To(ContainSubstring("--tls-server-auth streaming_replica=cluster-example"))
		Expect(starts()).To(HaveLen(1))
	})

	It("reloads after a consistent rotation, and not while it is inconsistent", func(ctx SpecContext) {
		runnable.tick(ctx)
		first := running()

		rotated := newTestKeyPair("cluster-example-rw")
		writeTestFile(runnable.CertFile, rotated.cert)
		runnable.tick(ctx)
		Consistently(reloads(first.pid)).WithTimeout(200 * time.Millisecond).WithPolling(processPolling).Should(BeZero())

		writeTestFile(runnable.KeyFile, rotated.key)
		runnable.tick(ctx)
		Eventually(reloads(first.pid)).WithTimeout(processTimeout).WithPolling(processPolling).Should(Equal(1))

		runnable.tick(ctx)
		Expect(running()).To(Equal(first))
		Expect(starts()).To(HaveLen(1))
		Expect(reloads(first.pid)()).To(Equal(1))
	})

	It("keeps the server running when only the log level changes", func(ctx SpecContext) {
		runnable.tick(ctx)
		first := running()

		updateArchive(ctx, func(archive *pgbackrestv1.Archive) {
			archive.Spec.Configuration.Log = &pgbackrestApi.LogConfiguration{LevelStderr: "info"}
		})
		runnable.refresh(ctx)
		Expect(runnable.desired.logLevel).To(Equal("info"))
		runnable.tick(ctx)

		Expect(running()).To(Equal(first))
		Expect(processRunning(first.pid)).To(BeTrue())
		Expect(starts()).To(HaveLen(1))
	})

	It("restarts with the new stanza", func(ctx SpecContext) {
		runnable.tick(ctx)
		first := running()

		var cluster cnpgv1.Cluster
		Expect(cli.Get(ctx, standbyClusterKey, &cluster)).To(Succeed())
		cluster.Spec.Plugins[0].Parameters["stanza"] = "custom"
		Expect(cli.Update(ctx, &cluster)).To(Succeed())
		runnable.refresh(ctx)
		runnable.tick(ctx)

		expectGone(first)
		second := running()
		Expect(second.pid).ToNot(Equal(first.pid))
		Expect(second.args).To(ContainSubstring("--tls-server-auth streaming_replica=custom"))
	})

	It("restarts with the common name of a new replication certificate", func(ctx SpecContext) {
		runnable.tick(ctx)
		first := running()

		writeTestFile(runnable.ReplicationCertFile, newTestKeyPair("custom-replica").cert)
		runnable.tick(ctx)

		expectGone(first)
		second := running()
		Expect(second.pid).ToNot(Equal(first.pid))
		Expect(second.args).To(ContainSubstring("--tls-server-auth custom-replica=cluster-example"))
	})

	It("stops when the feature is disabled", func(ctx SpecContext) {
		runnable.tick(ctx)
		first := running()

		updateArchive(ctx, func(archive *pgbackrestv1.Archive) {
			archive.Spec.Configuration.BackupStandby.Enabled = false
		})
		runnable.refresh(ctx)
		runnable.tick(ctx)

		Expect(runnable.server).To(BeNil())
		expectGone(first)
	})

	It("stops when the Archive is deleted", func(ctx SpecContext) {
		runnable.tick(ctx)
		first := running()

		Expect(cli.Delete(ctx, newStandbyArchive(nil))).To(Succeed())
		runnable.refresh(ctx)
		runnable.tick(ctx)

		Expect(runnable.server).To(BeNil())
		expectGone(first)
	})

	It("keeps running and reloading when the API cannot be reached", func(ctx SpecContext) {
		runnable.tick(ctx)
		first := running()

		failGets = true
		runnable.refresh(ctx)
		rotated := newTestKeyPair("cluster-example-rw")
		writeTestFile(runnable.CertFile, rotated.cert)
		writeTestFile(runnable.KeyFile, rotated.key)
		runnable.tick(ctx)

		Expect(running()).To(Equal(first))
		Eventually(reloads(first.pid)).WithTimeout(processTimeout).WithPolling(processPolling).Should(Equal(1))
	})

	It("keeps running when a read of the API does not return before the deadline", func(ctx SpecContext) {
		runnable.tick(ctx)
		first := running()
		desired := runnable.desired

		blockGets = true
		refreshCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		runnable.refresh(refreshCtx)
		Expect(runnable.desired).To(BeIdenticalTo(desired))

		runnable.tick(ctx)
		Expect(running()).To(Equal(first))
		Expect(processRunning(first.pid)).To(BeTrue())
	})

	It("stops when the stanza cannot be authorised", func(ctx SpecContext) {
		runnable.tick(ctx)
		first := running()

		var cluster cnpgv1.Cluster
		Expect(cli.Get(ctx, standbyClusterKey, &cluster)).To(Succeed())
		cluster.Spec.Plugins[0].Parameters["stanza"] = "*"
		Expect(cli.Update(ctx, &cluster)).To(Succeed())
		runnable.refresh(ctx)
		Expect(runnable.desired).To(BeNil())
		runnable.tick(ctx)

		Expect(runnable.server).To(BeNil())
		expectGone(first)
	})

	It("leaves the server alone while the client common name cannot be authorised", func(ctx SpecContext) {
		runnable.tick(ctx)
		first := running()

		writeTestFile(runnable.ReplicationCertFile, newTestKeyPair("a=b").cert)
		runnable.tick(ctx)
		Expect(runnable.certificatesError).To(ContainSubstring(`contains "="`))
		Expect(running()).To(Equal(first))
		Expect(processRunning(first.pid)).To(BeTrue())
		Expect(starts()).To(HaveLen(1))

		writeTestFile(runnable.ReplicationCertFile, newTestKeyPair(cnpgv1.StreamingReplicationUser).cert)
		runnable.tick(ctx)
		Expect(runnable.certificatesError).To(BeEmpty())
		Eventually(reloads(first.pid)).WithTimeout(processTimeout).WithPolling(processPolling).Should(Equal(1))
	})

	It("restarts after an unexpected exit on the next tick, not in the same one", func(ctx SpecContext) {
		runnable.tick(ctx)
		first := running()

		Expect(syscall.Kill(first.pid, syscall.SIGKILL)).To(Succeed())
		Eventually(func() *standbyServer {
			runnable.tick(ctx)
			return runnable.server
		}).WithTimeout(processTimeout).WithPolling(processPolling).Should(BeNil())
		Expect(starts()).To(HaveLen(1))
		expectGone(first)

		runnable.tick(ctx)
		Expect(running().pid).ToNot(Equal(first.pid))
	})

	It("kills a server that ignores SIGTERM together with its children", func(ctx SpecContext) {
		writeStandIn("")
		runnable.tick(ctx)
		first := running()

		runnable.stop(ctx)

		Expect(runnable.server).To(BeNil())
		expectGone(first)
	})
})
