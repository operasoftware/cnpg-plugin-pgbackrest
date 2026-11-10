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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/operator/config"
)

// SidecarConfigurationHashAnnotation holds, on every Cluster using the plugin, the hash of the
// instanceSidecarConfiguration of the Archives it refers to. CloudNativePG does not watch Archive
// objects: changing the annotation triggers the Cluster reconciliation, which detects the outdated
// sidecar in the instance pods and rolls them out according to the Cluster's update strategy.
const SidecarConfigurationHashAnnotation = "pgbackrest.cnpg.opera.com/sidecar-configuration-hash"

const clusterArchivesIndex = ".pgbackrest.archives"

// ArchiveReconciler reconciles an Archive object.
type ArchiveReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=create;patch;update;get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=create;patch;update;get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=create;list;get;watch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=backups,verbs=get;list;watch
// +kubebuilder:rbac:groups=pgbackrest.cnpg.opera.com,resources=archives,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=pgbackrest.cnpg.opera.com,resources=archives/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=pgbackrest.cnpg.opera.com,resources=archives/finalizers,verbs=update
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters/finalizers,verbs=update

// Reconcile refreshes SidecarConfigurationHashAnnotation on the Clusters referring to the Archive,
// so that CloudNativePG rolls out the instance pods running an outdated sidecar.
func (r *ArchiveReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	var clusters cnpgv1.ClusterList
	if err := r.List(ctx, &clusters,
		client.InNamespace(req.Namespace),
		client.MatchingFields{clusterArchivesIndex: req.Name},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("while listing clusters: %w", err)
	}

	var errs []error
	for i := range clusters.Items {
		cluster := &clusters.Items[i]
		hash, err := r.sidecarConfigurationHash(ctx, referredArchives(cluster))
		if err != nil {
			errs = append(errs, fmt.Errorf("while hashing the sidecar configuration of cluster %s: %w", cluster.Name, err))
			continue
		}
		if cluster.Annotations[SidecarConfigurationHashAnnotation] == hash {
			continue
		}

		origCluster := cluster.DeepCopy()
		if cluster.Annotations == nil {
			cluster.Annotations = map[string]string{}
		}
		cluster.Annotations[SidecarConfigurationHashAnnotation] = hash
		if err := r.Patch(ctx, cluster, client.MergeFrom(origCluster)); err != nil {
			if apierrs.IsNotFound(err) {
				continue
			}
			errs = append(errs, fmt.Errorf("while annotating cluster %s: %w", cluster.Name, err))
			continue
		}
		contextLogger.Info("Updated the sidecar configuration hash of a cluster",
			"cluster", cluster.Name, "hash", hash)
	}

	return ctrl.Result{}, errors.Join(errs...)
}

// sidecarConfigurationHash hashes the sidecar configuration of all the Archives a Cluster refers
// to: env is collected from all of them, and a per-cluster value lets the annotation converge
// when several Archives are reconciled.
func (r *ArchiveReconciler) sidecarConfigurationHash(
	ctx context.Context,
	archives []types.NamespacedName,
) (string, error) {
	keys := slices.Clone(archives)
	slices.SortFunc(keys, func(a, b types.NamespacedName) int { return strings.Compare(a.Name, b.Name) })

	sum := sha256.New()
	for _, key := range keys {
		var archive pgbackrestv1.Archive
		if err := r.Get(ctx, key, &archive); err != nil {
			if !apierrs.IsNotFound(err) {
				return "", err
			}
		}
		data, err := json.Marshal(archive.Spec.InstanceSidecarConfiguration)
		if err != nil {
			return "", err
		}
		sum.Write([]byte(key.Name + "\x00"))
		sum.Write(data)
		sum.Write([]byte("\x00"))
	}

	return hex.EncodeToString(sum.Sum(nil)[:8]), nil
}

// referredArchives returns the Archives a Cluster refers to. The archive of a disabled
// plugin entry is skipped: CloudNativePG does not load it, so it injects no sidecar.
func referredArchives(cluster *cnpgv1.Cluster) []types.NamespacedName {
	pluginConfiguration := config.NewFromCluster(cluster)
	enabled := slices.ContainsFunc(cluster.Spec.Plugins, func(p cnpgv1.PluginConfiguration) bool {
		return p.Name == metadata.PluginName && p.IsEnabled()
	})
	if !enabled {
		pluginConfiguration.PgbackrestObjectName = ""
	}
	return pluginConfiguration.GetReferredArchiveObjectsKey()
}

func indexClusterArchives(obj client.Object) []string {
	cluster, ok := obj.(*cnpgv1.Cluster)
	if !ok {
		return nil
	}
	archives := referredArchives(cluster)
	names := make([]string, 0, len(archives))
	for _, archive := range archives {
		names = append(names, archive.Name)
	}
	return names
}

// SetupWithManager sets up the controller with the Manager.
func (r *ArchiveReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(), &cnpgv1.Cluster{}, clusterArchivesIndex, indexClusterArchives,
	); err != nil {
		return fmt.Errorf("unable to index clusters by archive: %w", err)
	}

	err := ctrl.NewControllerManagedBy(mgr).
		For(&pgbackrestv1.Archive{}, builder.WithPredicates(sidecarConfigurationChangedPredicate())).
		Complete(r)
	if err != nil {
		return fmt.Errorf("unable to create controller: %w", err)
	}

	return nil
}

// sidecarConfigurationChangedPredicate drops the updates leaving instanceSidecarConfiguration
// unchanged. Create events are kept: on startup they catch up the changes made while the plugin
// was not running.
func sidecarConfigurationChangedPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldArchive, okOld := e.ObjectOld.(*pgbackrestv1.Archive)
			newArchive, okNew := e.ObjectNew.(*pgbackrestv1.Archive)
			if !okOld || !okNew {
				return false
			}
			return !equality.Semantic.DeepEqual(
				oldArchive.Spec.InstanceSidecarConfiguration,
				newArchive.Spec.InstanceSidecarConfiguration,
			)
		},
	}
}
