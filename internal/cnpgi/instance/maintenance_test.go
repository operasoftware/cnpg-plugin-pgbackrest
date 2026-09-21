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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/catalog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testNamespace  = "default"
	testCluster    = "cluster-example"
	testClusterUID = "0cbc1b6a-4c5e-4a5e-9f3e-6dcbb4a1f9a0"

	// deliberately different from the cluster name, which is what the stanza
	// defaults to, so that the two cannot be confused by the code under test
	testStanza = "cluster-example-stanza"

	keptBackupID    = "20260102-010101F"
	expiredBackupID = "20260101-010101F"
)

func buildMaintenanceScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	Expect(cnpgv1.AddToScheme(scheme)).To(Succeed())

	return scheme
}

func buildTestCluster() *cnpgv1.Cluster {
	return &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testCluster,
			UID:       types.UID(testClusterUID),
		},
	}
}

func buildTestConfiguration() *pgbackrestApi.PgbackrestConfiguration {
	return &pgbackrestApi.PgbackrestConfiguration{
		Repositories: []pgbackrestApi.PgbackrestRepository{
			{
				EndpointURL:     "minio:9000",
				Bucket:          "backups",
				DestinationPath: "/",
			},
		},
	}
}

func buildTestResultMetadata() backupResultMetadata {
	return newBackupResultMetadata(buildTestCluster(), testStanza, buildTestConfiguration())
}

// buildBackupResource creates a Backup resource taken by this plugin, for the test
// cluster, stanza and repositories, that completed with the given backup ID
func buildBackupResource(name string, backupID string) *cnpgv1.Backup {
	return &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      name,
			UID:       types.UID("uid-" + name),
		},
		Spec: cnpgv1.BackupSpec{
			Cluster: cnpgv1.LocalObjectReference{Name: testCluster},
			Method:  cnpgv1.BackupMethodPlugin,
		},
		Status: cnpgv1.BackupStatus{
			Phase:          cnpgv1.BackupPhaseCompleted,
			BackupID:       backupID,
			PluginMetadata: buildTestResultMetadata().toMap(),
		},
	}
}

var _ = Describe("Backup result metadata", func() {
	It("records the plugin and the location the backup was written to", func() {
		// the keys are what earlier resources were written with, so they are spelled
		// out here rather than taken from the constants they have to keep matching
		Expect(buildTestResultMetadata().toMap()).To(Equal(map[string]string{
			"name":         metadata.Data.Name,
			"version":      metadata.Data.Version,
			"displayName":  metadata.Data.DisplayName,
			"clusterUID":   testClusterUID,
			"stanza":       testStanza,
			"repositories": "minio:9000/backups/",
		}))
	})

	It("survives a round trip through the recorded map", func() {
		resultMetadata := buildTestResultMetadata()

		Expect(newBackupResultMetadataFromMap(resultMetadata.toMap())).To(Equal(resultMetadata))
	})

	It("describes the repositories in the order pgBackRest indexes them", func() {
		configuration := buildTestConfiguration()
		configuration.Repositories = append(configuration.Repositories, pgbackrestApi.PgbackrestRepository{
			EndpointURL:     "s3.example.com",
			Bucket:          "other-backups",
			DestinationPath: "/archive",
		})

		Expect(describeRepositories(configuration)).
			To(Equal("minio:9000/backups/,s3.example.com/other-backups/archive"))
	})

	DescribeTable("attributes a recorded metadata to a location",
		func(mutate func(*backupResultMetadata), expected bool) {
			current := buildTestResultMetadata()
			recorded := buildTestResultMetadata()
			mutate(&recorded)

			Expect(current.describesSameLocation(recorded)).To(Equal(expected))
		},
		Entry("the very same one", func(_ *backupResultMetadata) {}, true),
		Entry("another plugin", func(m *backupResultMetadata) { m.pluginName = "barman-cloud.cloudnative-pg.io" }, false),
		Entry("no plugin at all", func(m *backupResultMetadata) { m.pluginName = "" }, false),
		Entry("another cluster incarnation", func(m *backupResultMetadata) { m.clusterUID = "other-uid" }, false),
		Entry("another stanza", func(m *backupResultMetadata) { m.stanza = "other-stanza" }, false),
		Entry("another repository", func(m *backupResultMetadata) { m.repositories = "minio:9000/other/" }, false),
		Entry("an unrecorded cluster UID", func(m *backupResultMetadata) { m.clusterUID = "" }, true),
		Entry("an unrecorded stanza", func(m *backupResultMetadata) { m.stanza = "" }, true),
		Entry("unrecorded repositories", func(m *backupResultMetadata) { m.repositories = "" }, true),
	)
})

var _ = Describe("Backup resources reconciliation", func() {
	var (
		cluster          *cnpgv1.Cluster
		resultMetadata   backupResultMetadata
		backupCatalog    *catalog.Catalog
		repositories     int
		interceptorFuncs interceptor.Funcs
	)

	BeforeEach(func() {
		cluster = buildTestCluster()
		resultMetadata = buildTestResultMetadata()
		repositories = 1
		interceptorFuncs = interceptor.Funcs{}
		backupCatalog = &catalog.Catalog{
			Stanza:  testStanza,
			Backups: []catalog.PgbackrestBackup{{ID: keptBackupID}},
			Repos:   []catalog.PgbackrestRepo{{Key: 1}},
		}
	})

	// runReconcile runs the reconciliation against a client holding the given Backup
	// resources and returns the names of the ones that survived it, in every namespace
	runReconcile := func(ctx SpecContext, backups ...*cnpgv1.Backup) ([]string, error) {
		objects := make([]client.Object, len(backups))
		for idx, backupResource := range backups {
			objects[idx] = backupResource
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(buildMaintenanceScheme()).
			WithObjects(objects...).
			WithInterceptorFuncs(interceptorFuncs).
			Build()

		err := reconcileBackupsWithCatalog(
			ctx, fakeClient, cluster, resultMetadata, backupCatalog, repositories)

		var remaining cnpgv1.BackupList
		Expect(fakeClient.List(ctx, &remaining)).To(Succeed())

		names := make([]string, len(remaining.Items))
		for idx := range remaining.Items {
			names[idx] = remaining.Items[idx].Name
		}

		return names, err
	}

	reconcile := func(ctx SpecContext, backups ...*cnpgv1.Backup) []string {
		names, err := runReconcile(ctx, backups...)
		Expect(err).ToNot(HaveOccurred())

		return names
	}

	It("deletes the Backup resources whose data has been expired", func(ctx SpecContext) {
		expired := buildBackupResource("expired", expiredBackupID)
		kept := buildBackupResource("kept", keptBackupID)

		Expect(reconcile(ctx, expired, kept)).To(ConsistOf("kept"))
	})

	It("keeps the Backup resources of another cluster", func(ctx SpecContext) {
		foreign := buildBackupResource("foreign", expiredBackupID)
		foreign.Spec.Cluster.Name = "another-cluster"

		Expect(reconcile(ctx, foreign)).To(ConsistOf("foreign"))
	})

	It("keeps the Backup resources of another namespace", func(ctx SpecContext) {
		foreign := buildBackupResource("foreign", expiredBackupID)
		foreign.Namespace = "another-namespace"

		Expect(reconcile(ctx, foreign)).To(ConsistOf("foreign"))
	})

	It("keeps the Backup resources taken by another plugin", func(ctx SpecContext) {
		foreign := buildBackupResource("foreign", expiredBackupID)
		foreign.Status.PluginMetadata[pluginNameMetadataKey] = "barman-cloud.cloudnative-pg.io"

		Expect(reconcile(ctx, foreign)).To(ConsistOf("foreign"))
	})

	It("keeps the Backup resources without plugin metadata", func(ctx SpecContext) {
		foreign := buildBackupResource("foreign", expiredBackupID)
		foreign.Status.PluginMetadata = nil

		Expect(reconcile(ctx, foreign)).To(ConsistOf("foreign"))
	})

	It("keeps the Backup resources of a previous incarnation of the cluster", func(ctx SpecContext) {
		predecessor := buildBackupResource("predecessor", expiredBackupID)
		predecessor.Status.PluginMetadata[clusterUIDMetadataKey] = "a-previous-uid"

		Expect(reconcile(ctx, predecessor)).To(ConsistOf("predecessor"))
	})

	It("keeps the Backup resources taken from another stanza", func(ctx SpecContext) {
		foreign := buildBackupResource("foreign", expiredBackupID)
		foreign.Status.PluginMetadata[stanzaMetadataKey] = "another-stanza"

		Expect(reconcile(ctx, foreign)).To(ConsistOf("foreign"))
	})

	It("keeps the Backup resources written to other repositories", func(ctx SpecContext) {
		foreign := buildBackupResource("foreign", expiredBackupID)
		foreign.Status.PluginMetadata[repositoriesMetadataKey] = "minio:9000/an-older-bucket/"

		Expect(reconcile(ctx, foreign)).To(ConsistOf("foreign"))
	})

	It("deletes the expired Backup resources that predate the location being recorded",
		func(ctx SpecContext) {
			legacy := buildBackupResource("legacy", expiredBackupID)
			legacy.Status.PluginMetadata = map[string]string{pluginNameMetadataKey: metadata.Data.Name}

			Expect(reconcile(ctx, legacy)).To(BeEmpty())
		})

	It("keeps the Backup resources that are not completed", func(ctx SpecContext) {
		running := buildBackupResource("running", expiredBackupID)
		running.Status.Phase = cnpgv1.BackupPhaseRunning
		failed := buildBackupResource("failed", "")
		failed.Status.Phase = cnpgv1.BackupPhaseFailed

		Expect(reconcile(ctx, running, failed)).To(ConsistOf("running", "failed"))
	})

	It("keeps the Backup resources without a backup ID", func(ctx SpecContext) {
		unidentified := buildBackupResource("unidentified", "")

		Expect(reconcile(ctx, unidentified)).To(ConsistOf("unidentified"))
	})

	It("keeps every Backup resource when the catalog is empty", func(ctx SpecContext) {
		backupCatalog.Backups = nil
		expired := buildBackupResource("expired", expiredBackupID)

		Expect(reconcile(ctx, expired)).To(ConsistOf("expired"))
	})

	It("keeps every Backup resource when a repository could not be read", func(ctx SpecContext) {
		backupCatalog.Status = catalog.PgbackrestStanzaStatus{Code: 4, Message: "different across repos"}
		backupCatalog.Repos = []catalog.PgbackrestRepo{
			{Key: 1},
			{Key: 2, Status: catalog.PgbackrestStanzaStatus{Code: 99, Message: "other"}},
		}
		expired := buildBackupResource("expired", expiredBackupID)

		Expect(reconcile(ctx, expired)).To(ConsistOf("expired"))
	})

	It("keeps every Backup resource when the catalog describes fewer repositories",
		func(ctx SpecContext) {
			backupCatalog.Repos = nil
			expired := buildBackupResource("expired", expiredBackupID)

			Expect(reconcile(ctx, expired)).To(ConsistOf("expired"))
		})

	It("refuses to act on the catalog of another stanza", func(ctx SpecContext) {
		backupCatalog.Stanza = "another-stanza"
		expired := buildBackupResource("expired", expiredBackupID)

		names, err := runReconcile(ctx, expired)

		Expect(err).To(HaveOccurred())
		Expect(names).To(ConsistOf("expired"))
	})

	It("deletes what it can when deleting a resource fails", func(ctx SpecContext) {
		interceptorFuncs.Delete = func(
			ctx context.Context,
			cli client.WithWatch,
			obj client.Object,
			opts ...client.DeleteOption,
		) error {
			if obj.GetName() == "expired-a-stubborn" {
				return errors.New("no luck today")
			}

			return cli.Delete(ctx, obj, opts...)
		}

		// the resources are reconciled in the order they are listed, which is by name:
		// the one that fails comes first, so a loop that gave up on the first error
		// would leave the second one behind
		stubborn := buildBackupResource("expired-a-stubborn", expiredBackupID)
		expired := buildBackupResource("expired-b", expiredBackupID)

		names, err := runReconcile(ctx, stubborn, expired)

		Expect(err).To(MatchError(ContainSubstring("no luck today")))
		Expect(names).To(ConsistOf("expired-a-stubborn"))
	})

	It("deletes the very resource it listed", func(ctx SpecContext) {
		var recorded *metav1.Preconditions
		interceptorFuncs.Delete = func(
			ctx context.Context,
			cli client.WithWatch,
			obj client.Object,
			opts ...client.DeleteOption,
		) error {
			deleteOptions := &client.DeleteOptions{}
			for _, opt := range opts {
				opt.ApplyToDelete(deleteOptions)
			}
			recorded = deleteOptions.Preconditions

			return cli.Delete(ctx, obj, opts...)
		}

		expired := buildBackupResource("expired", expiredBackupID)

		Expect(reconcile(ctx, expired)).To(BeEmpty())
		Expect(recorded).ToNot(BeNil())
		Expect(recorded.UID).To(HaveValue(Equal(expired.UID)))
	})

	It("keeps every Backup resource when the location is not fully known", func(ctx SpecContext) {
		resultMetadata.clusterUID = ""
		expired := buildBackupResource("expired", expiredBackupID)

		names, err := runReconcile(ctx, expired)

		Expect(err).To(MatchError(ContainSubstring("incomplete backup location")))
		Expect(names).To(ConsistOf("expired"))
	})

	It("reconciles a stanza whose second repository holds no backup yet", func(ctx SpecContext) {
		repositories = 2
		backupCatalog.Status = catalog.PgbackrestStanzaStatus{Code: 4, Message: "different across repos"}
		backupCatalog.Repos = []catalog.PgbackrestRepo{
			{Key: 1},
			{Key: 2, Status: catalog.PgbackrestStanzaStatus{Code: 2, Message: "no valid backups"}},
		}
		expired := buildBackupResource("expired", expiredBackupID)

		Expect(reconcile(ctx, expired)).To(BeEmpty())
	})

	It("tolerates a resource replaced while it was reconciling", func(ctx SpecContext) {
		interceptorFuncs.Delete = func(
			_ context.Context,
			_ client.WithWatch,
			obj client.Object,
			_ ...client.DeleteOption,
		) error {
			return apierrs.NewConflict(
				cnpgv1.SchemeGroupVersion.WithResource("backups").GroupResource(),
				obj.GetName(),
				errors.New("the UID in the precondition does not match"),
			)
		}

		expired := buildBackupResource("expired", expiredBackupID)

		_, err := runReconcile(ctx, expired)

		Expect(err).ToNot(HaveOccurred())
	})

	It("tolerates a resource deleted while it was reconciling", func(ctx SpecContext) {
		interceptorFuncs.Delete = func(
			_ context.Context,
			_ client.WithWatch,
			obj client.Object,
			_ ...client.DeleteOption,
		) error {
			return apierrs.NewNotFound(
				cnpgv1.SchemeGroupVersion.WithResource("backups").GroupResource(),
				obj.GetName(),
			)
		}

		expired := buildBackupResource("expired", expiredBackupID)

		_, err := runReconcile(ctx, expired)

		Expect(err).ToNot(HaveOccurred())
	})
})
