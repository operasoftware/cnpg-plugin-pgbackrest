/*
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

package walarchive

import (
	"fmt"
	"strings"
	"time"

	v1 "github.com/cloudnative-pg/api/pkg/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	internalClient "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/client"
	internalCluster "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/cluster"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/command"
	internalLogs "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/logs"
	nmsp "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/namespace"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// lazyStanzaLogMessage is the message emitted by the WAL archive handler when it
// creates the pgbackrest stanza on the fly because it did not exist yet.
const lazyStanzaLogMessage = "created pgbackrest stanza so WAL archiving can start"

var _ = Describe("WAL archiving without a prior backup", func() {
	var namespace *corev1.Namespace
	var cl client.Client

	BeforeEach(func(ctx SpecContext) {
		var err error
		cl, _, err = internalClient.NewClient()
		Expect(err).NotTo(HaveOccurred())
		namespace, err = nmsp.CreateUniqueNamespace(ctx, cl, "wal-archive-no-backup")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func(ctx SpecContext) {
		Expect(cl.Delete(ctx, namespace)).To(Succeed())
	})

	It("creates the stanza lazily, archives WALs before any backup, and still supports backup and restore",
		func(ctx SpecContext) {
			testResources := createWalArchiveTestResources(namespace.Name)

			By("starting the object store deployment")
			Expect(testResources.ObjectStoreResources.Create(ctx, cl)).To(Succeed())

			By("creating the Archive")
			Expect(cl.Create(ctx, testResources.Archive)).To(Succeed())

			By("creating a CloudNativePG cluster that only enables WAL archiving (no backup)")
			cluster := testResources.Cluster
			Expect(cl.Create(ctx, cluster)).To(Succeed())

			By("waiting for the cluster to be ready")
			waitForClusterReady(ctx, cl, cluster)

			clientSet, cfg, err := internalClient.NewClientSet()
			Expect(err).NotTo(HaveOccurred())

			primaryPod := fmt.Sprintf("%s-1", cluster.Name)

			By("adding data to PostgreSQL")
			execPsql(ctx, clientSet, cfg, cluster.Namespace, primaryPod,
				"CREATE TABLE wal_test (id int, data text);")

			By("generating WAL files WITHOUT creating any backup first")
			// Each pg_switch_wal() forces a WAL segment to be archived. On a fresh
			// cluster the stanza does not exist yet, so the first archive must create
			// it lazily. Before the fix, this would fail indefinitely until a backup
			// was taken.
			for i := 0; i < 5; i++ {
				execPsql(ctx, clientSet, cfg, cluster.Namespace, primaryPod,
					fmt.Sprintf("INSERT INTO wal_test VALUES (%d, 'data-%d'); SELECT pg_switch_wal();", i, i))
				time.Sleep(500 * time.Millisecond)
			}

			By("verifying the stanza was created lazily and WALs were archived successfully")
			Eventually(func(g Gomega) {
				logs := getSidecarLogs(ctx, g, clientSet, cluster.Namespace, primaryPod)

				lazyStanzaEntries := internalLogs.FindLogEntriesByMessage(logs, lazyStanzaLogMessage)
				g.Expect(lazyStanzaEntries).NotTo(BeEmpty(),
					"the sidecar should have logged that it created the stanza on the first WAL archive")

				completedBatches := internalLogs.FindArchiveBatchCompletions(logs)
				g.Expect(completedBatches).NotTo(BeEmpty(),
					"there should be at least one completed WAL archive batch")

				g.Expect(hasSuccessfulArchiveBatch(completedBatches)).To(BeTrue(),
					"at least one WAL archive batch should have completed with a successful, error-free archive")
			}).WithTimeout(4 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

			By("taking a backup now to prove the backup path still works after lazy stanza creation")
			backup := testResources.Backup
			Expect(cl.Create(ctx, backup)).To(Succeed())
			waitForBackupCompleted(ctx, cl, backup)

			By("adding data after the backup so restore has WALs to replay")
			execPsql(ctx, clientSet, cfg, cluster.Namespace, primaryPod,
				"INSERT INTO wal_test VALUES (99, 'after-backup');")
			// The committed row lives in the current WAL segment. Capture it, force a
			// switch so it becomes archivable, then wait until the archiver has actually
			// uploaded it. Otherwise the restored cluster may finish recovery before the
			// segment reaches the object store and the post-backup row would be lost.
			postBackupWAL := queryPsqlOutput(ctx, clientSet, cfg, cluster.Namespace, primaryPod,
				"SELECT pg_walfile_name(pg_current_wal_lsn());")
			execPsql(ctx, clientSet, cfg, cluster.Namespace, primaryPod, "SELECT pg_switch_wal();")

			By("waiting for the post-backup WAL segment to be archived")
			Eventually(func(g Gomega) {
				lastArchived := queryPsqlOutputG(g, ctx, clientSet, cfg, cluster.Namespace, primaryPod,
					"SELECT COALESCE(last_archived_wal, '') FROM pg_stat_archiver;")
				g.Expect(lastArchived).NotTo(BeEmpty(), "no WAL has been archived yet")
				g.Expect(lastArchived >= postBackupWAL).To(BeTrue(),
					fmt.Sprintf("archived WAL %q has not yet reached the post-backup segment %q",
						lastArchived, postBackupWAL))
			}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

			By("restoring into a new cluster from the archive")
			restore := testResources.RestoreCluster
			Expect(cl.Create(ctx, restore)).To(Succeed())

			By("waiting for the restored cluster to be ready")
			waitForClusterReady(ctx, cl, restore)

			By("verifying the restored data is present")
			output := queryPsqlOutput(ctx, clientSet, cfg, restore.Namespace,
				fmt.Sprintf("%s-1", restore.Name), "SELECT count(*) FROM wal_test;")
			Expect(output).To(Equal("6"),
				"restored cluster should contain the 5 pre-backup rows plus the 1 post-backup row")
		})
})

// waitForClusterReady blocks until the given cluster reports a ready status.
func waitForClusterReady(ctx SpecContext, cl client.Client, cluster *v1.Cluster) {
	Eventually(func(g Gomega) {
		g.Expect(cl.Get(ctx,
			types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
			cluster)).To(Succeed())
		g.Expect(internalCluster.IsReady(*cluster)).To(BeTrue())
	}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}

// waitForBackupCompleted blocks until the given backup reaches the completed phase.
func waitForBackupCompleted(ctx SpecContext, cl client.Client, backup *v1.Backup) {
	Eventually(func(g Gomega) {
		g.Expect(cl.Get(ctx,
			types.NamespacedName{Name: backup.Name, Namespace: backup.Namespace},
			backup)).To(Succeed())
		g.Expect(backup.Status.Phase).To(BeEquivalentTo(v1.BackupPhaseCompleted))
	}).Within(3 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}

// execPsql runs a SQL statement in the postgres container of the given pod.
func execPsql(
	ctx SpecContext,
	clientSet *kubernetes.Clientset,
	cfg *rest.Config,
	namespace, podName, sql string,
) {
	_, _, err := command.ExecuteInContainer(ctx,
		*clientSet,
		cfg,
		command.ContainerLocator{
			NamespaceName: namespace,
			PodName:       podName,
			ContainerName: "postgres",
		},
		nil,
		[]string{"psql", "-tAc", sql})
	Expect(err).NotTo(HaveOccurred())
}

// queryPsqlOutput runs a SQL query and returns its trimmed stdout, failing the spec on error.
func queryPsqlOutput(
	ctx SpecContext,
	clientSet *kubernetes.Clientset,
	cfg *rest.Config,
	namespace, podName, sql string,
) string {
	return queryPsqlOutputG(Default, ctx, clientSet, cfg, namespace, podName, sql)
}

// queryPsqlOutputG is like queryPsqlOutput but reports failures to the provided
// Gomega, so it can be used safely inside an Eventually block.
func queryPsqlOutputG(
	g Gomega,
	ctx SpecContext,
	clientSet *kubernetes.Clientset,
	cfg *rest.Config,
	namespace, podName, sql string,
) string {
	out, _, err := command.ExecuteInContainer(ctx,
		*clientSet,
		cfg,
		command.ContainerLocator{
			NamespaceName: namespace,
			PodName:       podName,
			ContainerName: "postgres",
		},
		nil,
		[]string{"psql", "-tAc", sql})
	g.Expect(err).NotTo(HaveOccurred())
	return strings.TrimSpace(out)
}

// getSidecarLogs retrieves the parsed JSON logs of the plugin-pgbackrest sidecar.
func getSidecarLogs(
	ctx SpecContext,
	g Gomega,
	clientSet *kubernetes.Clientset,
	namespace, podName string,
) []map[string]any {
	logs, err := internalLogs.GetPodContainerLogs(ctx, clientSet, namespace, podName, "plugin-pgbackrest", nil)
	g.Expect(err).NotTo(HaveOccurred())
	return logs
}

// hasSuccessfulArchiveBatch reports whether any completed batch archived at least
// one WAL file with no failures.
func hasSuccessfulArchiveBatch(completedBatches []map[string]any) bool {
	for _, batch := range completedBatches {
		successful, okS := batch["successfulArchives"].(float64)
		failed, okF := batch["failedArchives"].(float64)
		if okS && okF && successful >= 1 && failed == 0 {
			return true
		}
	}
	return false
}
