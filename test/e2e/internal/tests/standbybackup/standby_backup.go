/*
Copyright 2024, The CloudNativePG Contributors
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

package standbybackup

import (
	"fmt"
	"slices"
	"time"

	v1 "github.com/cloudnative-pg/api/pkg/api/v1"
	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	internalClient "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/client"
	internalCluster "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/cluster"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/command"
	nmsp "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/namespace"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/objectstore"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Backup from a standby", func() {
	var namespace *corev1.Namespace
	var cl client.Client
	BeforeEach(func(ctx SpecContext) {
		var err error
		cl, _, err = internalClient.NewClient()
		Expect(err).NotTo(HaveOccurred())
		namespace, err = nmsp.CreateUniqueNamespace(ctx, cl, "standby-backup")
		Expect(err).NotTo(HaveOccurred())
	})
	AfterEach(func(ctx SpecContext) {
		Expect(cl.Delete(ctx, namespace)).To(Succeed())
	})

	It("should back up from a standby, restore the backup and keep working after a switchover", func(ctx SpecContext) {
		waitForReady := func(cluster *v1.Cluster) {
			GinkgoHelper()
			Eventually(func(g Gomega) {
				g.Expect(cl.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
					cluster)).To(Succeed())
				g.Expect(internalCluster.IsReady(*cluster)).To(BeTrue())
			}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
		}
		// Returns the pod the backup ran on once the backup has completed.
		backupOnStandby := func(name string) string {
			GinkgoHelper()
			backup := newStandbyBackup(namespace.Name, name)
			Expect(cl.Create(ctx, backup)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(cl.Get(ctx, types.NamespacedName{Name: backup.Name, Namespace: backup.Namespace},
					backup)).To(Succeed())
				g.Expect(backup.Status.Phase).To(BeEquivalentTo(v1.BackupPhaseCompleted),
					"backup error: %q", backup.Status.Error)
			}).Within(3 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
			Expect(backup.Status.InstanceID).NotTo(BeNil())

			return backup.Status.InstanceID.PodName
		}

		By("starting the object store deployment")
		objectStore := objectstore.NewMinioObjectStoreResources(namespace.Name, minio)
		Expect(objectStore.Create(ctx, cl)).To(Succeed())

		// The sidecars start the pgBackRest TLS server only if the Archive
		// already allows it when they read their configuration.
		By("creating the Archive with backups from a standby enabled")
		Expect(cl.Create(ctx, newArchive(namespace.Name))).To(Succeed())

		By("creating a two-instance cluster with the managed pgbackrest service")
		src := newSrcCluster(namespace.Name)
		Expect(cl.Create(ctx, src)).To(Succeed())
		waitForReady(src)

		By("adding data on the primary")
		clientSet, cfg, err := internalClient.NewClientSet()
		Expect(err).NotTo(HaveOccurred())
		_, _, err = command.ExecuteInContainer(ctx, *clientSet, cfg,
			command.ContainerLocator{
				NamespaceName: src.Namespace,
				PodName:       src.Status.CurrentPrimary,
				ContainerName: "postgres",
			},
			nil,
			[]string{"psql", "-tAc", "CREATE TABLE test (i int); INSERT INTO test SELECT generate_series(1, 100);"})
		Expect(err).NotTo(HaveOccurred())

		By("taking a backup that is dispatched to a standby")
		firstPod := backupOnStandby(firstBackupName)
		Expect(firstPod).NotTo(BeEmpty())
		Expect(firstPod).NotTo(Equal(src.Status.CurrentPrimary))

		By("archiving the WAL written after the backup")
		_, _, err = command.ExecuteInContainer(ctx, *clientSet, cfg,
			command.ContainerLocator{
				NamespaceName: src.Namespace,
				PodName:       src.Status.CurrentPrimary,
				ContainerName: "postgres",
			},
			nil,
			[]string{"psql", "-tAc", "SELECT pg_switch_wal()"})
		Expect(err).NotTo(HaveOccurred())

		By("restoring the backup into a new cluster")
		dst := newRestoreCluster(namespace.Name)
		Expect(cl.Create(ctx, dst)).To(Succeed())
		waitForReady(dst)

		By("verifying the data in the restored cluster")
		output, _, err := command.ExecuteInContainer(ctx, *clientSet, cfg,
			command.ContainerLocator{
				NamespaceName: dst.Namespace,
				PodName:       dst.Status.CurrentPrimary,
				ContainerName: "postgres",
			},
			nil,
			[]string{"psql", "-tAc", "SELECT count(*) FROM test;"})
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(BeEquivalentTo("100\n"))

		By("switching over to the standby")
		Expect(cl.Get(ctx, types.NamespacedName{Name: src.Name, Namespace: src.Namespace}, src)).To(Succeed())
		formerPrimary := src.Status.CurrentPrimary
		standbyIdx := slices.IndexFunc(src.Status.InstanceNames, func(name string) bool {
			return name != formerPrimary
		})
		Expect(standbyIdx).To(BeNumerically(">=", 0), "no standby among %v", src.Status.InstanceNames)
		newPrimary := src.Status.InstanceNames[standbyIdx]
		Expect(newPrimary).To(Equal(firstPod))
		// Same status change as `kubectl cnpg promote`.
		patch := client.MergeFrom(src.DeepCopy())
		src.Status.TargetPrimary = newPrimary
		src.Status.TargetPrimaryTimestamp = pgTime.GetCurrentTimestamp()
		src.Status.Phase = v1.PhaseSwitchover
		src.Status.PhaseReason = fmt.Sprintf("Switching over to %v", newPrimary)
		Expect(cl.Status().Patch(ctx, src, patch)).To(Succeed())

		By("waiting for the former standby to become the primary")
		Eventually(func(g Gomega) {
			g.Expect(cl.Get(ctx, types.NamespacedName{Name: src.Name, Namespace: src.Namespace}, src)).To(Succeed())
			g.Expect(src.Status.CurrentPrimary).To(Equal(newPrimary))
			g.Expect(internalCluster.IsReady(*src)).To(BeTrue())
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

		By("taking another backup after the switchover")
		secondPod := backupOnStandby(secondBackupName)
		Expect(secondPod).To(Equal(formerPrimary))
	})
})
