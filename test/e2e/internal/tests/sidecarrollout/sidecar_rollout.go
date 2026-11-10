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

package sidecarrollout

import (
	"time"

	cloudnativepgv1 "github.com/cloudnative-pg/api/pkg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pluginPgbackrestV1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/controller"
	internalClient "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/client"
	internalCluster "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/cluster"
	nmsp "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/namespace"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/objectstore"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	minio           = "minio"
	clusterName     = "sidecar-rollout"
	archiveName     = "sidecar-rollout"
	pluginContainer = "plugin-pgbackrest"
)

var _ = Describe("Archive sidecar configuration change", func() {
	var namespace *corev1.Namespace
	var cl client.Client

	BeforeEach(func(ctx SpecContext) {
		var err error
		cl, _, err = internalClient.NewClient()
		Expect(err).NotTo(HaveOccurred())
		namespace, err = nmsp.CreateUniqueNamespace(ctx, cl, "sidecar-rollout")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func(ctx SpecContext) {
		Expect(cl.Delete(ctx, namespace)).To(Succeed())
	})

	It("should roll out the instance pods with the new sidecar resources", func(ctx SpecContext) {
		By("starting the object store deployment")
		Expect(objectstore.NewMinioObjectStoreResources(namespace.Name, minio).Create(ctx, cl)).To(Succeed())

		By("creating the Archive")
		archive := objectstore.NewMinioArchive(namespace.Name, archiveName, minio, 1)
		Expect(cl.Create(ctx, archive)).To(Succeed())

		By("creating a CloudNativePG cluster")
		cluster := newCluster(namespace.Name)
		Expect(cl.Create(ctx, cluster)).To(Succeed())

		By("having the cluster ready")
		Eventually(func(g Gomega) {
			g.Expect(cl.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
			g.Expect(internalCluster.IsReady(*cluster)).To(BeTrue())
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

		hashBefore := cluster.Annotations[controller.SidecarConfigurationHashAnnotation]

		By("changing only the sidecar resources in the Archive")
		newLimit := resource.MustParse("512Mi")
		Eventually(func(g Gomega) {
			var current pluginPgbackrestV1.Archive
			g.Expect(cl.Get(ctx, types.NamespacedName{Name: archiveName, Namespace: namespace.Name}, &current)).
				To(Succeed())
			current.Spec.InstanceSidecarConfiguration.Resources.Limits = corev1.ResourceList{
				corev1.ResourceMemory: newLimit,
			}
			g.Expect(cl.Update(ctx, &current)).To(Succeed())
		}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())

		By("having the plugin annotate the cluster")
		Eventually(func(g Gomega) {
			g.Expect(cl.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
			g.Expect(cluster.Annotations[controller.SidecarConfigurationHashAnnotation]).
				NotTo(BeElementOf("", hashBefore))
		}).WithTimeout(time.Minute).WithPolling(2 * time.Second).Should(Succeed())

		By("having every instance pod recreated with the new sidecar limit")
		Eventually(func(g Gomega) {
			var pods corev1.PodList
			g.Expect(cl.List(ctx, &pods,
				client.InNamespace(namespace.Name),
				client.MatchingLabels{
					utils.ClusterLabelName: clusterName,
					utils.PodRoleLabelName: string(utils.PodRoleInstance),
				},
			)).To(Succeed())
			g.Expect(pods.Items).To(HaveLen(int(cluster.Spec.Instances)))
			for _, pod := range pods.Items {
				g.Expect(sidecarMemoryLimit(pod)).To(Equal(newLimit.String()), "pod %s", pod.Name)
			}

			g.Expect(cl.Get(ctx, client.ObjectKeyFromObject(cluster), cluster)).To(Succeed())
			g.Expect(internalCluster.IsReady(*cluster)).To(BeTrue())
		}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
	})
})

func sidecarMemoryLimit(pod corev1.Pod) string {
	for _, container := range pod.Spec.InitContainers {
		if container.Name == pluginContainer {
			return container.Resources.Limits.Memory().String()
		}
	}
	return ""
}

func newCluster(namespace string) *cloudnativepgv1.Cluster {
	return &cloudnativepgv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: cloudnativepgv1.ClusterSpec{
			Instances:       2,
			ImagePullPolicy: corev1.PullAlways,
			Plugins: []cloudnativepgv1.PluginConfiguration{
				{
					Name: "pgbackrest.cnpg.opera.com",
					Parameters: map[string]string{
						"pgbackrestObjectName": archiveName,
					},
				},
			},
			StorageConfiguration: cloudnativepgv1.StorageConfiguration{
				Size: "1Gi",
			},
		},
	}
}
