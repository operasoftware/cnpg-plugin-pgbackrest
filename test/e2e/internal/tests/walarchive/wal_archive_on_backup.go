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
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	internalClient "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/client"
	internalLogs "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/logs"
	nmsp "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/namespace"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WAL archiving with createStanza set to OnBackup", func() {
	var namespace *corev1.Namespace
	var cl client.Client

	BeforeEach(func(ctx SpecContext) {
		var err error
		cl, _, err = internalClient.NewClient()
		Expect(err).NotTo(HaveOccurred())
		namespace, err = nmsp.CreateUniqueNamespace(ctx, cl, "wal-archive-on-backup")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func(ctx SpecContext) {
		Expect(cl.Delete(ctx, namespace)).To(Succeed())
	})

	It("does not create the stanza on WAL archive, so archiving fails until a backup creates it",
		func(ctx SpecContext) {
			testResources := createWalArchiveTestResources(namespace.Name)
			// Opt out of lazy stanza creation. With OnBackup the WAL archive path must not
			// create the stanza, so archiving stays broken until a backup runs.
			testResources.Archive.Spec.Configuration.CreateStanza = pgbackrestApi.StanzaCreateOnBackup

			By("starting the object store deployment")
			Expect(testResources.ObjectStoreResources.Create(ctx, cl)).To(Succeed())

			By("creating the Archive with createStanza=OnBackup")
			Expect(cl.Create(ctx, testResources.Archive)).To(Succeed())

			By("creating a CloudNativePG cluster that only enables WAL archiving (no backup)")
			cluster := testResources.Cluster
			Expect(cl.Create(ctx, cluster)).To(Succeed())

			By("waiting for the cluster to be ready")
			waitForClusterReady(ctx, cl, cluster)

			clientSet, cfg, err := internalClient.NewClientSet()
			Expect(err).NotTo(HaveOccurred())

			primaryPod := fmt.Sprintf("%s-1", cluster.Name)

			By("adding data and forcing WAL switches without any backup")
			execPsql(ctx, clientSet, cfg, cluster.Namespace, primaryPod,
				"CREATE TABLE wal_test (id int, data text);")
			for i := 0; i < 5; i++ {
				execPsql(ctx, clientSet, cfg, cluster.Namespace, primaryPod,
					fmt.Sprintf("INSERT INTO wal_test VALUES (%d, 'data-%d'); SELECT pg_switch_wal();", i, i))
				time.Sleep(500 * time.Millisecond)
			}

			By("verifying WAL archiving fails because the stanza was not created on archive")
			Eventually(func(g Gomega) {
				failed := queryPsqlOutputG(g, ctx, clientSet, cfg, cluster.Namespace, primaryPod,
					"SELECT failed_count FROM pg_stat_archiver;")
				g.Expect(failed).NotTo(Equal("0"),
					"archive-push should be failing while the stanza does not exist")

				lastArchived := queryPsqlOutputG(g, ctx, clientSet, cfg, cluster.Namespace, primaryPod,
					"SELECT COALESCE(last_archived_wal, '') FROM pg_stat_archiver;")
				g.Expect(lastArchived).To(BeEmpty(), "no WAL should be archived before a backup under OnBackup")

				logs := getSidecarLogs(ctx, g, clientSet, cluster.Namespace, primaryPod)
				g.Expect(internalLogs.FindLogEntriesByMessage(logs, lazyStanzaLogMessage)).To(BeEmpty(),
					"the sidecar must not create the stanza on WAL archive when createStanza=OnBackup")
			}).WithTimeout(3 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

			By("taking a backup, which creates the stanza under OnBackup")
			backup := testResources.Backup
			Expect(cl.Create(ctx, backup)).To(Succeed())
			waitForBackupCompleted(ctx, cl, backup)

			By("verifying WAL archiving recovers once the backup created the stanza")
			execPsql(ctx, clientSet, cfg, cluster.Namespace, primaryPod,
				"INSERT INTO wal_test VALUES (99, 'after-backup'); SELECT pg_switch_wal();")
			Eventually(func(g Gomega) {
				lastArchived := queryPsqlOutputG(g, ctx, clientSet, cfg, cluster.Namespace, primaryPod,
					"SELECT COALESCE(last_archived_wal, '') FROM pg_stat_archiver;")
				g.Expect(lastArchived).NotTo(BeEmpty(),
					"WAL archiving should succeed after the backup created the stanza")
			}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
		})
})
