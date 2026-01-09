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

package parallelarchive

import (
	"fmt"
	"time"

	v1 "github.com/cloudnative-pg/api/pkg/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	internalClient "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/client"
	internalCluster "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/cluster"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/command"
	internalLogs "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/logs"
	nmsp "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/namespace"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Parallel WAL Archive", func() {
	var namespace *corev1.Namespace
	var cl client.Client

	BeforeEach(func(ctx SpecContext) {
		var err error
		cl, _, err = internalClient.NewClient()
		Expect(err).NotTo(HaveOccurred())
		namespace, err = nmsp.CreateUniqueNamespace(ctx, cl, "parallel-archive")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func(ctx SpecContext) {
		Expect(cl.Delete(ctx, namespace)).To(Succeed())
	})

	It("should archive multiple WAL files when maxParallel is set", func(ctx SpecContext) {
		// This test verifies that when wal.maxParallel is configured,
		// multiple WAL files are gathered and archived together.

		By("creating test resources with maxParallel=4")
		testResources := createParallelArchiveTestResources(namespace.Name, 4)

		By("starting the object store deployment")
		Expect(testResources.ObjectStoreResources.Create(ctx, cl)).To(Succeed())

		By("creating the Archive with maxParallel=4")
		Expect(cl.Create(ctx, testResources.Archive)).To(Succeed())

		By("creating a CloudNativePG cluster")
		cluster := testResources.Cluster
		Expect(cl.Create(ctx, cluster)).To(Succeed())

		By("waiting for the cluster to be ready")
		Eventually(func(g Gomega) {
			g.Expect(cl.Get(
				ctx,
				types.NamespacedName{
					Name:      cluster.Name,
					Namespace: cluster.Namespace,
				},
				cluster)).To(Succeed())
			g.Expect(internalCluster.IsReady(*cluster)).To(BeTrue())
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

		clientSet, cfg, err := internalClient.NewClientSet()
		Expect(err).NotTo(HaveOccurred())

		By("adding initial data to PostgreSQL")
		_, _, err = command.ExecuteInContainer(ctx,
			*clientSet,
			cfg,
			command.ContainerLocator{
				NamespaceName: cluster.Namespace,
				PodName:       fmt.Sprintf("%s-1", cluster.Name),
				ContainerName: "postgres",
			},
			nil,
			[]string{"psql", "-tAc", "CREATE TABLE parallel_test (id int, data text);"})
		Expect(err).NotTo(HaveOccurred())

		By("creating a backup to initialize pgBackRest stanza")
		backup := testResources.Backup
		Expect(cl.Create(ctx, backup)).To(Succeed())

		By("waiting for the backup to complete")
		Eventually(func(g Gomega) {
			g.Expect(cl.Get(ctx, types.NamespacedName{Name: backup.Name, Namespace: backup.Namespace},
				backup)).To(Succeed())
			g.Expect(backup.Status.Phase).To(BeEquivalentTo(v1.BackupPhaseCompleted))
		}).Within(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

		By("rapidly generating multiple WAL files to queue them for archiving")
		// Generate 10 WAL switches in quick succession to create a backlog
		// that can be processed in parallel
		for i := 0; i < 10; i++ {
			_, _, err := command.ExecuteInContainer(ctx,
				*clientSet,
				cfg,
				command.ContainerLocator{
					NamespaceName: cluster.Namespace,
					PodName:       fmt.Sprintf("%s-1", cluster.Name),
					ContainerName: "postgres",
				},
				nil,
				[]string{"psql", "-tAc", fmt.Sprintf("INSERT INTO parallel_test VALUES (%d, 'data-%d'); SELECT pg_switch_wal();", i, i)})
			Expect(err).NotTo(HaveOccurred())

			// Small delay to ensure files are queued but not immediately archived
			time.Sleep(100 * time.Millisecond)
		}

		By("waiting a moment for archiving to complete")
		time.Sleep(10 * time.Second)

		By("retrieving and parsing container logs")
		logs, err := internalLogs.GetPodContainerLogs(
			ctx,
			clientSet,
			cluster.Namespace,
			fmt.Sprintf("%s-1", cluster.Name),
			"plugin-pgbackrest",
			nil,
		)
		Expect(err).NotTo(HaveOccurred())

		By("finding WAL archive batch prepared entries")
		preparedBatches := internalLogs.FindArchiveBatches(logs)
		Expect(preparedBatches).NotTo(BeEmpty(), "should have at least one WAL archive batch prepared entry")

		By("finding WAL archive batch completed entries")
		completedBatches := internalLogs.FindArchiveBatchCompletions(logs)
		Expect(completedBatches).NotTo(BeEmpty(), "should have at least one WAL archive batch completed entry")

		By("verifying that at least some batches contain multiple WAL files")
		foundMultiFilePrepareBatch := false
		for _, batch := range preparedBatches {
			if walFiles, ok := batch["walFiles"].([]interface{}); ok {
				if len(walFiles) > 1 {
					foundMultiFilePrepareBatch = true
					GinkgoWriter.Printf("Found parallel archive batch with %d WAL files (requested: %v)\n",
						len(walFiles), batch["requestedWalFile"])
					break
				}
			}
		}
		Expect(foundMultiFilePrepareBatch).To(BeTrue(),
			"at least one batch should have prepared multiple WAL files for archiving in parallel")

		By("verifying successful parallel archive completions")
		foundMultiFileCompleteBatch := false
		for _, batch := range completedBatches {
			if successfulArchives, ok := batch["successfulArchives"].(float64); ok {
				if int(successfulArchives) > 1 {
					foundMultiFileCompleteBatch = true
					GinkgoWriter.Printf("Found parallel archive completion with %d successful archives (requested: %v)\n",
						int(successfulArchives), batch["requestedWalFile"])
					break
				}
			}
		}
		Expect(foundMultiFileCompleteBatch).To(BeTrue(),
			"at least one batch should have completed archiving multiple WAL files in parallel")
	})

})
