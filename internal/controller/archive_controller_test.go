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

package controller

import (
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func newArchive(namespace, name, memoryLimit string) *pgbackrestv1.Archive {
	return &pgbackrestv1.Archive{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: pgbackrestv1.ArchiveSpec{
			InstanceSidecarConfiguration: pgbackrestv1.InstanceSidecarConfiguration{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(memoryLimit)},
				},
			},
		},
	}
}

func newClusterUsingArchive(namespace, name, archiveName string) *cnpgv1.Cluster {
	return &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: cnpgv1.ClusterSpec{
			Plugins: []cnpgv1.PluginConfiguration{{
				Name:       metadata.PluginName,
				Parameters: map[string]string{"pgbackrestObjectName": archiveName},
			}},
		},
	}
}

func withRecoveryArchive(cluster *cnpgv1.Cluster, archiveName string) *cnpgv1.Cluster {
	cluster.Spec.Bootstrap = &cnpgv1.BootstrapConfiguration{
		Recovery: &cnpgv1.BootstrapRecovery{Source: "origin"},
	}
	cluster.Spec.ExternalClusters = []cnpgv1.ExternalCluster{{
		Name: "origin",
		PluginConfiguration: &cnpgv1.PluginConfiguration{
			Name:       metadata.PluginName,
			Parameters: map[string]string{"pgbackrestObjectName": archiveName},
		},
	}}
	return cluster
}

var _ = Describe("ArchiveReconciler", func() {
	var (
		k8sClient  client.Client
		reconciler *ArchiveReconciler
	)

	BeforeEach(func() {
		scheme := runtime.NewScheme()
		Expect(cnpgv1.AddToScheme(scheme)).To(Succeed())
		Expect(pgbackrestv1.AddToScheme(scheme)).To(Succeed())

		disabled := newClusterUsingArchive("default", "disabled", "archive")
		disabled.Spec.Plugins[0].Enabled = ptr.To(false)

		k8sClient = fake.NewClientBuilder().WithScheme(scheme).
			WithIndex(&cnpgv1.Cluster{}, clusterArchivesIndex, indexClusterArchives).
			WithObjects(
			newArchive("default", "archive", "1Gi"),
			newArchive("default", "recovery", "1Gi"),
			newArchive("other-ns", "archive", "1Gi"),
			newClusterUsingArchive("default", "using", "archive"),
			newClusterUsingArchive("default", "other", "other-archive"),
			newClusterUsingArchive("other-ns", "using", "archive"),
			withRecoveryArchive(newClusterUsingArchive("default", "restored", "archive"), "recovery"),
			disabled,
		).Build()
		reconciler = &ArchiveReconciler{Client: k8sClient, Scheme: scheme}
	})

	reconcileArchive := func(ctx SpecContext, name string) {
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
	}

	getCluster := func(ctx SpecContext, namespace, name string) *cnpgv1.Cluster {
		var cluster cnpgv1.Cluster
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &cluster)).To(Succeed())
		return &cluster
	}

	It("annotates only the clusters referring to the archive", func(ctx SpecContext) {
		reconcileArchive(ctx, "archive")

		Expect(getCluster(ctx, "default", "using").Annotations).To(HaveKey(SidecarConfigurationHashAnnotation))
		Expect(getCluster(ctx, "default", "other").Annotations).NotTo(HaveKey(SidecarConfigurationHashAnnotation))
		Expect(getCluster(ctx, "other-ns", "using").Annotations).NotTo(HaveKey(SidecarConfigurationHashAnnotation))
		Expect(getCluster(ctx, "default", "disabled").Annotations).NotTo(HaveKey(SidecarConfigurationHashAnnotation))
	})

	It("changes the annotation only when the sidecar configuration changes", func(ctx SpecContext) {
		reconcileArchive(ctx, "archive")
		first := getCluster(ctx, "default", "using")

		reconcileArchive(ctx, "archive")
		Expect(getCluster(ctx, "default", "using").ResourceVersion).To(Equal(first.ResourceVersion))

		var archive pgbackrestv1.Archive
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "archive", Namespace: "default"}, &archive)).
			To(Succeed())
		archive.Spec.InstanceSidecarConfiguration.Resources.Limits[corev1.ResourceMemory] = resource.MustParse("2Gi")
		Expect(k8sClient.Update(ctx, &archive)).To(Succeed())

		reconcileArchive(ctx, "archive")
		Expect(getCluster(ctx, "default", "using").Annotations[SidecarConfigurationHashAnnotation]).
			NotTo(Equal(first.Annotations[SidecarConfigurationHashAnnotation]))
	})

	It("converges on one value for a cluster referring to several archives", func(ctx SpecContext) {
		reconcileArchive(ctx, "archive")
		afterArchive := getCluster(ctx, "default", "restored")
		Expect(afterArchive.Annotations).To(HaveKey(SidecarConfigurationHashAnnotation))

		reconcileArchive(ctx, "recovery")
		Expect(getCluster(ctx, "default", "restored").ResourceVersion).To(Equal(afterArchive.ResourceVersion))
	})
})

var _ = Describe("sidecarConfigurationChangedPredicate", func() {
	p := sidecarConfigurationChangedPredicate()

	It("passes updates of the sidecar configuration", func() {
		Expect(p.Update(event.UpdateEvent{
			ObjectOld: newArchive("default", "archive", "1Gi"),
			ObjectNew: newArchive("default", "archive", "2Gi"),
		})).To(BeTrue())
	})

	It("drops updates leaving the sidecar configuration unchanged", func() {
		updated := newArchive("default", "archive", "1024Mi")
		updated.Spec.Configuration.Compression = "zst"
		Expect(p.Update(event.UpdateEvent{
			ObjectOld: newArchive("default", "archive", "1Gi"),
			ObjectNew: updated,
		})).To(BeFalse())
	})

	It("passes creations, to catch up the changes made while the plugin was down", func() {
		Expect(p.Create(event.CreateEvent{Object: newArchive("default", "archive", "1Gi")})).To(BeTrue())
	})
})
