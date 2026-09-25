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
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/catalog"
)

const (
	testNamespace  = "default"
	testClusterUID = types.UID("cluster-uid-1")
	testStanza     = "cluster-example"
)

var (
	testConfiguration = pgbackrestApi.PgbackrestConfiguration{
		Repositories: []pgbackrestApi.PgbackrestRepository{
			{EndpointURL: "https://s3.example.com", Bucket: "bucket", DestinationPath: "/cluster-example"},
		},
	}
	testLocation = newBackupResultMetadata(testClusterUID, testStanza, &testConfiguration)
)

func newTestCluster() *cnpgv1.Cluster {
	return &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "cluster-example",
			UID:       testClusterUID,
		},
	}
}

// newTestBackup builds a completed plugin backup of the given cluster with
// this plugin's metadata. Tests override single fields to build the
// negative cases. The cluster label mirrors what CloudNativePG sets on
// every Backup it creates.
func newTestBackup(name, clusterName, backupID string) *cnpgv1.Backup {
	return &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      name,
			Labels:    map[string]string{cnpgutils.ClusterLabelName: clusterName},
		},
		Spec: cnpgv1.BackupSpec{
			Cluster: cnpgv1.LocalObjectReference{Name: clusterName},
		},
		Status: cnpgv1.BackupStatus{
			Phase:          cnpgv1.BackupPhaseCompleted,
			Method:         cnpgv1.BackupMethodPlugin,
			BackupID:       backupID,
			PluginMetadata: testLocation.toMap(),
		},
	}
}

func remainingBackupNames(ctx context.Context, cli client.Client) []string {
	var list cnpgv1.BackupList
	ExpectWithOffset(1, cli.List(ctx, &list, client.InNamespace(testNamespace))).To(Succeed())
	names := make([]string, 0, len(list.Items))
	for _, b := range list.Items {
		names = append(names, b.Name)
	}
	return names
}

var _ = Describe("catalogUsable", func() {
	usable := func() *catalog.Catalog {
		return &catalog.Catalog{
			Stanza:  testStanza,
			Status:  catalog.PgbackrestStanzaStatus{Code: catalog.StanzaStatusCodeOk},
			Repos:   []catalog.PgbackrestRepo{{Key: 1, Status: catalog.PgbackrestStanzaStatus{Code: catalog.StanzaStatusCodeOk}}},
			Backups: []catalog.PgbackrestBackup{{ID: "20250101-000000F"}},
		}
	}

	It("accepts a complete catalog of the expected stanza with at least one backup", func() {
		Expect(catalogUsable(usable(), testStanza, 1)).To(Succeed())
	})

	It("rejects a catalog of another stanza", func() {
		Expect(catalogUsable(usable(), "other-stanza", 1)).To(MatchError(ContainSubstring("stanza")))
	})

	It("rejects a catalog that does not describe every configured repository", func() {
		c := usable()
		c.Repos = append(c.Repos, catalog.PgbackrestRepo{Key: 2, Status: catalog.PgbackrestStanzaStatus{Code: 99}})
		c.Status.Code = catalog.StanzaStatusCodeMixed
		Expect(catalogUsable(c, testStanza, 2)).To(MatchError(ContainSubstring("repositor")))
	})

	It("reports the status of every repository when the catalog is incomplete", func() {
		c := usable()
		c.Repos = append(c.Repos, catalog.PgbackrestRepo{Key: 2, Status: catalog.PgbackrestStanzaStatus{Code: 99}})
		c.Status.Code = catalog.StanzaStatusCodeMixed
		Expect(catalogUsable(c, testStanza, 2)).To(MatchError(ContainSubstring("1=0 2=99")))
	})

	It("rejects a catalog whose stanza has not been created yet", func() {
		c := usable()
		c.Status.Code = catalog.StanzaStatusCodeMissing
		c.Repos[0].Status.Code = catalog.StanzaStatusCodeMissing
		c.Backups = nil
		Expect(catalogUsable(c, testStanza, 1)).To(MatchError(ContainSubstring("repositor")))
	})

	It("rejects an empty catalog", func() {
		c := usable()
		c.Backups = nil
		c.Status.Code = catalog.StanzaStatusCodeNoBackup
		c.Repos[0].Status.Code = catalog.StanzaStatusCodeNoBackup
		Expect(catalogUsable(c, testStanza, 1)).To(MatchError(errEmptyCatalog))
	})
})

var _ = Describe("useSameBackupLocation", func() {
	cluster := newTestCluster()

	It("accepts a plugin backup of this cluster taken by this plugin", func() {
		status := newTestBackup("b", cluster.Name, "id").Status
		Expect(useSameBackupLocation(&status, testLocation)).To(BeTrue())
	})

	It("rejects non-plugin backup methods", func() {
		status := newTestBackup("b", cluster.Name, "id").Status
		status.Method = cnpgv1.BackupMethodBarmanObjectStore
		Expect(useSameBackupLocation(&status, testLocation)).To(BeFalse())
	})

	It("rejects a different cluster UID", func() {
		status := newTestBackup("b", cluster.Name, "id").Status
		status.PluginMetadata = newBackupResultMetadata(types.UID("other-uid"), testStanza, &testConfiguration).toMap()
		Expect(useSameBackupLocation(&status, testLocation)).To(BeFalse())
	})

	It("rejects a different plugin", func() {
		status := newTestBackup("b", cluster.Name, "id").Status
		status.PluginMetadata["pluginName"] = "barman-cloud.cloudnative-pg.io"
		Expect(useSameBackupLocation(&status, testLocation)).To(BeFalse())
	})

	It("rejects backups without plugin metadata", func() {
		status := newTestBackup("b", cluster.Name, "id").Status
		status.PluginMetadata = nil
		Expect(useSameBackupLocation(&status, testLocation)).To(BeFalse())
	})

	It("rejects backups from older plugin versions without clusterUID", func() {
		status := newTestBackup("b", cluster.Name, "id").Status
		status.PluginMetadata = map[string]string{
			"version":     "0.5.2",
			"name":        metadata.Data.Name,
			"displayName": metadata.Data.DisplayName,
		}
		Expect(useSameBackupLocation(&status, testLocation)).To(BeFalse())
	})

	It("rejects a backup taken against another stanza", func() {
		status := newTestBackup("b", cluster.Name, "id").Status
		status.PluginMetadata = newBackupResultMetadata(testClusterUID, "another-stanza", &testConfiguration).toMap()
		Expect(useSameBackupLocation(&status, testLocation)).To(BeFalse())
	})

	It("rejects backups from older plugin versions without stanza", func() {
		status := newTestBackup("b", cluster.Name, "id").Status
		delete(status.PluginMetadata, "stanza")
		Expect(useSameBackupLocation(&status, testLocation)).To(BeFalse())
	})

	It("rejects a backup written to other repositories", func() {
		status := newTestBackup("b", cluster.Name, "id").Status
		status.PluginMetadata["repositories"] = "https://other/bucket/cluster-example"
		Expect(useSameBackupLocation(&status, testLocation)).To(BeFalse())
	})

	It("rejects backups from older plugin versions without repositories", func() {
		status := newTestBackup("b", cluster.Name, "id").Status
		delete(status.PluginMetadata, "repositories")
		Expect(useSameBackupLocation(&status, testLocation)).To(BeFalse())
	})
})

var _ = Describe("deleteBackupsNotInCatalog", func() {
	var cluster *cnpgv1.Cluster

	BeforeEach(func() {
		cluster = newTestCluster()
	})

	It("deletes completed backups whose ID is not in the catalog and keeps the others", func(ctx SpecContext) {
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			newTestBackup("in-catalog", cluster.Name, "20250101-000000F"),
			newTestBackup("stale", cluster.Name, "20240101-000000F"),
		).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, []string{"20250101-000000F"}, time.Now())).To(Succeed())
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("in-catalog"))
	})

	It("keeps backups of other clusters", func(ctx SpecContext) {
		// Labelled for our cluster (so it is listed) but spec'd for another one, to
		// exercise the Spec.Cluster.Name guard rather than the label filter.
		other := newTestBackup("other", cluster.Name, "stale-id")
		other.Spec.Cluster.Name = "another-cluster"
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(other).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("other"))
	})

	It("keeps backups that are not completed", func(ctx SpecContext) {
		failed := newTestBackup("failed", cluster.Name, "")
		failed.Status.Phase = cnpgv1.BackupPhaseFailed
		started := newTestBackup("started", cluster.Name, "")
		started.Status.Phase = cnpgv1.BackupPhaseStarted
		pending := newTestBackup("pending", cluster.Name, "")
		pending.Status.Phase = cnpgv1.BackupPhasePending
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(failed, started, pending).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("failed", "started", "pending"))
	})

	It("keeps backups with a different clusterUID", func(ctx SpecContext) {
		recreated := newTestBackup("old-incarnation", cluster.Name, "stale-id")
		recreated.Status.PluginMetadata = newBackupResultMetadata(types.UID("previous-uid"), testStanza, &testConfiguration).toMap()
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(recreated).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("old-incarnation"))
	})

	It("keeps a backup that completed after the catalog was read", func(ctx SpecContext) {
		catalogReadAt := time.Now()
		fresh := newTestBackup("fresh", cluster.Name, "fresh-id")
		fresh.Status.StoppedAt = &metav1.Time{Time: catalogReadAt.Add(time.Minute)}
		older := newTestBackup("older", cluster.Name, "older-id")
		older.Status.StoppedAt = &metav1.Time{Time: catalogReadAt.Add(-time.Minute)}
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(fresh, older).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, catalogReadAt)).To(Succeed())
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("fresh"))
	})

	It("keeps backups taken against another stanza", func(ctx SpecContext) {
		moved := newTestBackup("other-stanza", cluster.Name, "stale-id")
		moved.Status.PluginMetadata = newBackupResultMetadata(testClusterUID, "another-stanza", &testConfiguration).toMap()
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(moved).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("other-stanza"))
	})

	It("keeps backups written to other repositories", func(ctx SpecContext) {
		moved := newTestBackup("other-repositories", cluster.Name, "stale-id")
		otherConfiguration := pgbackrestApi.PgbackrestConfiguration{
			Repositories: []pgbackrestApi.PgbackrestRepository{
				{EndpointURL: "https://other", Bucket: "bucket", DestinationPath: "/cluster-example"},
			},
		}
		moved.Status.PluginMetadata = newBackupResultMetadata(testClusterUID, testStanza, &otherConfiguration).toMap()
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(moved).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("other-repositories"))
	})

	It("keeps backups without plugin metadata", func(ctx SpecContext) {
		legacy := newTestBackup("legacy", cluster.Name, "stale-id")
		legacy.Status.PluginMetadata = nil
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(legacy).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("legacy"))
	})

	It("keeps every backup whose recorded location does not match the current one", func(ctx SpecContext) {
		// The two shapes the cycle reports together: a backup written elsewhere and
		// one from a plugin version that recorded no location at all.
		moved := newTestBackup("other-repositories", cluster.Name, "stale-1")
		otherConfiguration := pgbackrestApi.PgbackrestConfiguration{
			Repositories: []pgbackrestApi.PgbackrestRepository{
				{EndpointURL: "https://other", Bucket: "bucket", DestinationPath: "/cluster-example"},
			},
		}
		moved.Status.PluginMetadata = newBackupResultMetadata(testClusterUID, testStanza, &otherConfiguration).toMap()
		legacy := newTestBackup("legacy", cluster.Name, "stale-2")
		legacy.Status.PluginMetadata = nil
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(moved, legacy).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("other-repositories", "legacy"))
	})

	It("keeps backups taken with another method", func(ctx SpecContext) {
		barman := newTestBackup("barman", cluster.Name, "stale-id")
		barman.Status.Method = cnpgv1.BackupMethodBarmanObjectStore
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(barman).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("barman"))
	})

	It("ignores NotFound on delete", func(ctx SpecContext) {
		cli := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(newTestBackup("stale", cluster.Name, "stale-id")).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
					return apierrors.NewNotFound(
						schema.GroupResource{Group: "postgresql.cnpg.io", Resource: "backups"}, obj.GetName())
				},
			}).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
	})

	It("ignores Conflict on delete", func(ctx SpecContext) {
		cli := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(newTestBackup("recreated", cluster.Name, "stale-id")).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
					return apierrors.NewConflict(
						schema.GroupResource{Group: "postgresql.cnpg.io", Resource: "backups"}, obj.GetName(),
						errors.New("the UID in the precondition does not match"))
				},
			}).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
	})

	It("deletes with the UID of the listed object as a precondition", func(ctx SpecContext) {
		var seen *types.UID
		stale := newTestBackup("stale", cluster.Name, "stale-id")
		stale.UID = types.UID("uid-stale")
		cli := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(stale).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					options := &client.DeleteOptions{}
					options.ApplyOptions(opts)
					if options.Preconditions != nil {
						seen = options.Preconditions.UID
					}
					return c.Delete(ctx, obj, opts...)
				},
			}).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(seen).NotTo(BeNil())
		Expect(*seen).NotTo(BeEmpty())
	})

	It("keeps completed backups that carry no backup ID", func(ctx SpecContext) {
		cli := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(newTestBackup("no-id", cluster.Name, "")).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("no-id"))
	})

	It("keeps deleting after a failure and returns an aggregated error", func(ctx SpecContext) {
		cli := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(
				newTestBackup("fails", cluster.Name, "stale-1"),
				newTestBackup("succeeds", cluster.Name, "stale-2"),
			).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					if obj.GetName() == "fails" {
						return errors.New("boom")
					}
					return c.Delete(ctx, obj, opts...)
				},
			}).Build()

		err := deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("default/fails"))
		Expect(err.Error()).To(ContainSubstring("boom"))
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("fails"))
	})

	It("lists only the Backups labelled with the cluster name", func(ctx SpecContext) {
		var selector string
		cli := fake.NewClientBuilder().WithScheme(scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					options := &client.ListOptions{}
					options.ApplyOptions(opts)
					if options.LabelSelector != nil {
						selector = options.LabelSelector.String()
					}
					return c.List(ctx, list, opts...)
				},
			}).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(selector).To(Equal("cnpg.io/cluster=cluster-example"))
	})

	It("walks every page of the list", func(ctx SpecContext) {
		// The fake client does not paginate; emulate two pages with the interceptor.
		calls := 0
		cli := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(newTestBackup("stale-1", cluster.Name, "s1"), newTestBackup("stale-2", cluster.Name, "s2")).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					calls++
					if err := c.List(ctx, list, opts...); err != nil {
						return err
					}
					backups := list.(*cnpgv1.BackupList)
					if calls == 1 {
						// First page: one object and a token. The second call returns whatever
						// is still there (the first object is gone by then) and no token.
						backups.Items = backups.Items[:1]
						backups.Continue = "page-2"
					} else {
						backups.Continue = ""
					}
					return nil
				},
			}).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(calls).To(Equal(2))
		Expect(remainingBackupNames(ctx, cli)).To(BeEmpty())
	})

	It("restarts the pass once when the continue token expired", func(ctx SpecContext) {
		calls := 0
		cli := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(newTestBackup("stale", cluster.Name, "s1")).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					calls++
					options := &client.ListOptions{}
					options.ApplyOptions(opts)
					switch {
					case calls == 1:
						if err := c.List(ctx, list, opts...); err != nil {
							return err
						}
						list.(*cnpgv1.BackupList).Items = nil
						list.(*cnpgv1.BackupList).Continue = "expired"
						return nil
					case options.Continue == "expired":
						return apierrors.NewResourceExpired("the continue token has expired")
					default:
						return c.List(ctx, list, opts...)
					}
				},
			}).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(calls).To(Equal(3))
		Expect(remainingBackupNames(ctx, cli)).To(BeEmpty())
	})

	It("forgets the delete errors of the pass aborted by an expired continue token", func(ctx SpecContext) {
		listCalls := 0
		deleteCalls := 0
		cli := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(newTestBackup("stale", cluster.Name, "s1")).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					listCalls++
					options := &client.ListOptions{}
					options.ApplyOptions(opts)
					switch {
					case listCalls == 1:
						if err := c.List(ctx, list, opts...); err != nil {
							return err
						}
						list.(*cnpgv1.BackupList).Continue = "expired"
						return nil
					case options.Continue == "expired":
						return apierrors.NewResourceExpired("the continue token has expired")
					default:
						return c.List(ctx, list, opts...)
					}
				},
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					deleteCalls++
					if deleteCalls == 1 {
						return errors.New("boom")
					}
					return c.Delete(ctx, obj, opts...)
				},
			}).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(deleteCalls).To(Equal(2))
		Expect(remainingBackupNames(ctx, cli)).To(BeEmpty())
	})

	It("keeps backups without the cluster label", func(ctx SpecContext) {
		unlabelled := newTestBackup("stale-unlabelled", cluster.Name, "stale-id")
		unlabelled.Labels = nil
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(unlabelled).Build()

		Expect(deleteBackupsNotInCatalog(ctx, cli, cluster, testLocation, nil, time.Now())).To(Succeed())
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("stale-unlabelled"))
	})
})

func newTestArchive(intervalSeconds *int32) *pgbackrestv1.Archive {
	return &pgbackrestv1.Archive{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "archive",
		},
		Spec: pgbackrestv1.ArchiveSpec{
			Configuration: testConfiguration,
			InstanceSidecarConfiguration: pgbackrestv1.InstanceSidecarConfiguration{
				CatalogMaintenanceIntervalSeconds: intervalSeconds,
			},
		},
	}
}

// newTestClusterWithPlugin enables this plugin on the cluster and points it
// at the given archive. primary is Status.CurrentPrimary.
func newTestClusterWithPlugin(archiveName, primary string) *cnpgv1.Cluster {
	cluster := newTestCluster()
	params := map[string]string{}
	if archiveName != "" {
		params["pgbackrestObjectName"] = archiveName
	}
	cluster.Spec.Plugins = []cnpgv1.PluginConfiguration{{
		Name:       metadata.PluginName,
		Parameters: params,
	}}
	cluster.Status.CurrentPrimary = primary
	return cluster
}

var _ = Describe("nextInterval", func() {
	It("keeps a positive period", func() {
		Expect(nextInterval(30 * time.Minute)).To(Equal(30 * time.Minute))
	})

	It("falls back to the default for zero", func() {
		Expect(nextInterval(0)).To(Equal(defaultCatalogMaintenanceInterval))
	})

	It("falls back to the default for negative values", func() {
		Expect(nextInterval(-5 * time.Second)).To(Equal(defaultCatalogMaintenanceInterval))
	})
})

var _ = Describe("maintenanceInterval", func() {
	It("defaults to the CRD default and stays enabled when unset", func() {
		interval, enabled := maintenanceInterval(newTestArchive(nil))
		Expect(enabled).To(BeTrue())
		Expect(interval).To(Equal(defaultCatalogMaintenanceInterval))
	})

	It("honours a configured interval", func() {
		interval, enabled := maintenanceInterval(newTestArchive(ptr.To[int32](900)))
		Expect(enabled).To(BeTrue())
		Expect(interval).To(Equal(15 * time.Minute))
	})

	It("disables maintenance and returns the default when set to zero", func() {
		interval, enabled := maintenanceInterval(newTestArchive(ptr.To[int32](0)))
		Expect(enabled).To(BeFalse())
		Expect(interval).To(Equal(defaultCatalogMaintenanceInterval))
	})
})

var _ = Describe("CatalogMaintenanceRunnable.cycle", func() {
	const podName = "cluster-example-1"

	newRunnable := func(cli client.Client) *CatalogMaintenanceRunnable {
		return &CatalogMaintenanceRunnable{
			Client:         cli,
			ClusterKey:     types.NamespacedName{Namespace: testNamespace, Name: "cluster-example"},
			CurrentPodName: podName,
		}
	}

	It("returns an error when the cluster does not exist", func(ctx SpecContext) {
		cli := fake.NewClientBuilder().WithScheme(scheme).Build()

		period, err := newRunnable(cli).cycle(ctx)

		Expect(err).To(HaveOccurred())
		Expect(period).To(BeZero())
	})

	It("skips the cycle when the plugin is not enabled on the cluster", func(ctx SpecContext) {
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newTestCluster()).Build()

		period, err := newRunnable(cli).cycle(ctx)

		Expect(err).NotTo(HaveOccurred())
		Expect(period).To(Equal(defaultCatalogMaintenanceInterval))
	})

	It("returns an error when the archive name is missing", func(ctx SpecContext) {
		cli := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(newTestClusterWithPlugin("", podName)).Build()

		_, err := newRunnable(cli).cycle(ctx)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("pgbackrestObjectName"))
	})

	It("returns an error when the archive does not exist", func(ctx SpecContext) {
		cli := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(newTestClusterWithPlugin("archive", podName)).Build()

		_, err := newRunnable(cli).cycle(ctx)

		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("skips maintenance on a replica", func(ctx SpecContext) {
		// On a replica no pgbackrest command runs, so the fake client is enough
		// to drive the whole cycle. The stale backup must survive.
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			newTestClusterWithPlugin("archive", "cluster-example-2"),
			newTestArchive(ptr.To[int32](900)),
			newTestBackup("stale", "cluster-example", "stale-id"),
		).Build()

		period, err := newRunnable(cli).cycle(ctx)

		Expect(err).NotTo(HaveOccurred())
		// The Archive is not read on a replica, so the interval it configures never comes into play.
		Expect(period).To(Equal(defaultCatalogMaintenanceInterval))
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("stale"))
	})

	It("does not read the Archive on a replica", func(ctx SpecContext) {
		archiveReads := 0
		cli := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(newTestClusterWithPlugin("archive", "cluster-example-2"), newTestArchive(ptr.To[int32](900))).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, isArchive := obj.(*pgbackrestv1.Archive); isArchive {
						archiveReads++
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}).Build()

		period, err := newRunnable(cli).cycle(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(archiveReads).To(BeZero())
		Expect(period).To(Equal(defaultCatalogMaintenanceInterval))
	})

	It("does nothing when maintenance is disabled", func(ctx SpecContext) {
		// Primary pod, disabled interval, a stale backup: no pgbackrest call is made (the
		// test environment has none), the backup survives, the Archive is polled again later.
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
			newTestClusterWithPlugin("archive", podName), newTestArchive(ptr.To[int32](0)),
			newTestBackup("stale", "cluster-example", "stale-id")).Build()
		period, err := newRunnable(cli).cycle(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(period).To(Equal(defaultCatalogMaintenanceInterval))
		Expect(remainingBackupNames(ctx, cli)).To(ConsistOf("stale"))
	})
})

var _ = Describe("initialDelay", func() {
	It("stays inside the default maintenance interval", func() {
		for range 100 {
			delay := initialDelay()
			Expect(delay).To(BeNumerically(">=", 0))
			Expect(delay).To(BeNumerically("<", defaultCatalogMaintenanceInterval))
		}
	})
})

var _ = Describe("CatalogMaintenanceRunnable.Start", func() {
	It("returns when the context is cancelled", func() {
		// Start must return nil promptly once the context is cancelled, whether
		// it is sleeping or between cycles.
		cli := fake.NewClientBuilder().WithScheme(scheme).Build()
		runnable := &CatalogMaintenanceRunnable{
			Client:         cli,
			ClusterKey:     types.NamespacedName{Namespace: testNamespace, Name: "cluster-example"},
			CurrentPodName: "cluster-example-1",
		}
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan error, 1)
		go func() { done <- runnable.Start(ctx) }()
		cancel()

		Eventually(done, "2s").Should(Receive(BeNil()))
	})
})
