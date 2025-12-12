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

package operator

import (
	"context"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cnpg-i-machinery/pkg/pluginhelper/decoder"
	"github.com/cloudnative-pg/cnpg-i-machinery/pkg/pluginhelper/object"
	"github.com/cloudnative-pg/cnpg-i/pkg/reconciler"
	"github.com/cloudnative-pg/machinery/pkg/log"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/operator/config"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/operator/specs"
)

// ReconcilerImplementation implements the Reconciler capability
type ReconcilerImplementation struct {
	Client client.Client
	reconciler.UnimplementedReconcilerHooksServer
}

// GetCapabilities implements the Reconciler interface
func (r ReconcilerImplementation) GetCapabilities(
	_ context.Context,
	_ *reconciler.ReconcilerHooksCapabilitiesRequest,
) (*reconciler.ReconcilerHooksCapabilitiesResult, error) {
	return &reconciler.ReconcilerHooksCapabilitiesResult{
		ReconcilerCapabilities: []*reconciler.ReconcilerHooksCapability{
			{
				Kind: reconciler.ReconcilerHooksCapability_KIND_CLUSTER,
			},
			{
				Kind: reconciler.ReconcilerHooksCapability_KIND_BACKUP,
			},
		},
	}, nil
}

// Pre implements the reconciler interface
func (r ReconcilerImplementation) Pre(
	ctx context.Context,
	request *reconciler.ReconcilerHooksRequest,
) (*reconciler.ReconcilerHooksResult, error) {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("Pre hook reconciliation start")
	reconciledKind, err := object.GetKind(request.GetResourceDefinition())
	if err != nil {
		return nil, err
	}
	if reconciledKind != "Cluster" {
		return &reconciler.ReconcilerHooksResult{
			Behavior: reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
		}, nil
	}

	contextLogger.Debug("parsing cluster definition")
	var cluster cnpgv1.Cluster
	if err := decoder.DecodeObjectLenient(
		request.GetResourceDefinition(),
		&cluster); err != nil {
		return nil, err
	}

	contextLogger = contextLogger.WithValues("name", cluster.Name, "namespace", cluster.Namespace)
	ctx = log.IntoContext(ctx, contextLogger)

	pluginConfiguration := config.NewFromCluster(&cluster)

	contextLogger.Debug("parsing pgbackrest archive object configuration")

	archiveObjects := make([]pgbackrestv1.Archive, 0, len(pluginConfiguration.GetReferredArchiveObjectsKey()))
	for _, archiveObjectKey := range pluginConfiguration.GetReferredArchiveObjectsKey() {
		var archiveObject pgbackrestv1.Archive
		if err := r.Client.Get(ctx, archiveObjectKey, &archiveObject); err != nil {
			if apierrs.IsNotFound(err) {
				contextLogger.Info(
					"pgbackrest archive object configuration not found, requeuing",
					"name", pluginConfiguration.PgbackrestObjectName,
					"namespace", cluster.Namespace)
				return &reconciler.ReconcilerHooksResult{
					Behavior: reconciler.ReconcilerHooksResult_BEHAVIOR_REQUEUE,
				}, nil
			}

			return nil, err
		}

		archiveObjects = append(archiveObjects, archiveObject)
	}

	if err := r.ensureRole(ctx, &cluster, archiveObjects); err != nil {
		return nil, err
	}

	if err := r.ensureRoleBinding(ctx, &cluster); err != nil {
		return nil, err
	}

	contextLogger.Info("Pre hook reconciliation completed")
	return &reconciler.ReconcilerHooksResult{
		Behavior: reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
	}, nil
}

// Post implements the reconciler interface
func (r ReconcilerImplementation) Post(
	ctx context.Context,
	_ *reconciler.ReconcilerHooksRequest,
) (*reconciler.ReconcilerHooksResult, error) {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("Post hook reconciliation start")
	contextLogger.Info("Post hook reconciliation completed")
	return &reconciler.ReconcilerHooksResult{
		Behavior: reconciler.ReconcilerHooksResult_BEHAVIOR_CONTINUE,
	}, nil
}

func (r ReconcilerImplementation) ensureRole(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
	archiveObjects []pgbackrestv1.Archive,
) error {
	contextLogger := log.FromContext(ctx)
	newRole := specs.BuildRole(cluster, archiveObjects)

	var role rbacv1.Role
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: newRole.Namespace,
		Name:      newRole.Name,
	}, &role); err != nil {
		if !apierrs.IsNotFound(err) {
			return err
		}

		contextLogger.Info(
			"Creating role",
			"name", newRole.Name,
			"namespace", newRole.Namespace,
		)

		if err := ctrl.SetControllerReference(
			cluster,
			newRole,
			r.Client.Scheme(),
		); err != nil {
			return err
		}

		return r.Client.Create(ctx, newRole)
	}

	if equality.Semantic.DeepEqual(newRole.Rules, role.Rules) {
		// There's no need to hit the API server again
		return nil
	}

	contextLogger.Info(
		"Patching role",
		"name", newRole.Name,
		"namespace", newRole.Namespace,
		"rules", newRole.Rules,
	)

	patch := client.MergeFrom(role.DeepCopy())
	role.Rules = newRole.Rules
	return r.Client.Patch(ctx, &role, patch)
}

func (r ReconcilerImplementation) ensureRoleBinding(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
) error {
	var role rbacv1.RoleBinding
	if err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      specs.GetRBACName(cluster.Name),
	}, &role); err != nil {
		if apierrs.IsNotFound(err) {
			return r.createRoleBinding(ctx, cluster)
		}
		return err
	}

	// TODO: this assumes role bindings never change.
	// Is that true? Should we relax this assumption?
	return nil
}

func (r ReconcilerImplementation) createRoleBinding(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
) error {
	roleBinding := specs.BuildRoleBinding(cluster)
	if err := ctrl.SetControllerReference(cluster, roleBinding, r.Client.Scheme()); err != nil {
		return err
	}
	return r.Client.Create(ctx, roleBinding)
}
