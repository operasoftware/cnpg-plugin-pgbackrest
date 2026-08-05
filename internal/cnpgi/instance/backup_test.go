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
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	pgbackrestCommand "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/command"
)

var _ = Describe("resolveStandbyTopology", func() {
	const (
		ns         = "test-ns"
		primaryPod = "cluster-1"
		standby    = "cluster-2"
		pgData     = "/var/lib/postgresql/data/pgdata"
	)

	newCluster := func(currentPrimary string) *cnpgv1.Cluster {
		c := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "cluster"}}
		c.Status.CurrentPrimary = currentPrimary
		return c
	}

	newImpl := func(instanceName string, objs ...client.Object) BackupServiceImplementation {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		return BackupServiceImplementation{Client: fakeClient, InstanceName: instanceName, PGDataPath: pgData}
	}

	cfg := func(enabled bool) *pgbackrestApi.PgbackrestConfiguration {
		return &pgbackrestApi.PgbackrestConfiguration{
			BackupStandby: &pgbackrestApi.BackupStandbyConfiguration{Enabled: enabled},
		}
	}

	It("returns nil when the feature is disabled", func(ctx SpecContext) {
		impl := newImpl(standby)
		topo, err := impl.resolveStandbyTopology(ctx, newCluster(primaryPod), cfg(false))
		Expect(err).ToNot(HaveOccurred())
		Expect(topo).To(BeNil())
	})

	It("returns nil (local backup) when this instance is the primary", func(ctx SpecContext) {
		impl := newImpl(primaryPod)
		topo, err := impl.resolveStandbyTopology(ctx, newCluster(primaryPod), cfg(true))
		Expect(err).ToNot(HaveOccurred())
		Expect(topo).To(BeNil())
	})

	It("returns nil when no primary is known yet", func(ctx SpecContext) {
		impl := newImpl(standby)
		topo, err := impl.resolveStandbyTopology(ctx, newCluster(""), cfg(true))
		Expect(err).ToNot(HaveOccurred())
		Expect(topo).To(BeNil())
	})

	It("builds the topology from the primary pod IP when on a standby", func(ctx SpecContext) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: primaryPod},
			Status:     corev1.PodStatus{PodIP: "10.0.0.7"},
		}
		impl := newImpl(standby, pod)
		topo, err := impl.resolveStandbyTopology(ctx, newCluster(primaryPod), cfg(true))
		Expect(err).ToNot(HaveOccurred())
		Expect(topo).ToNot(BeNil())
		Expect(*topo).To(Equal(pgbackrestCommand.StandbyBackupTopology{
			PrimaryHost:   "10.0.0.7",
			PrimaryPort:   pgbackrestCommand.DefaultServerPort,
			PrimaryPGData: pgData,
			CertFile:      pgbackrestCommand.DefaultTLSCertFile,
			KeyFile:       pgbackrestCommand.DefaultTLSKeyFile,
			CAFile:        pgbackrestCommand.DefaultTLSCAFile,
		}))
	})

	It("errors when the primary pod has no IP yet", func(ctx SpecContext) {
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: primaryPod}}
		impl := newImpl(standby, pod)
		_, err := impl.resolveStandbyTopology(ctx, newCluster(primaryPod), cfg(true))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no IP"))
	})

	It("errors when the primary pod cannot be found", func(ctx SpecContext) {
		impl := newImpl(standby) // primary pod not present in the fake client
		_, err := impl.resolveStandbyTopology(ctx, newCluster(primaryPod), cfg(true))
		Expect(err).To(HaveOccurred())
	})
})
