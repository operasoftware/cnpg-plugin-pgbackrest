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

package specs

import (
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
)

// BuildRole builds the Role object for this cluster
func BuildRole(
	cluster *cnpgv1.Cluster,
	pgbackrestObjects []pgbackrestv1.Archive,
) *rbacv1.Role {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      GetRBACName(cluster.Name),
		},

		Rules: []rbacv1.PolicyRule{},
	}

	secretsSet := stringset.New()
	pgbackrestObjectsSet := stringset.New()

	for _, pgbackrestObject := range pgbackrestObjects {
		pgbackrestObjectsSet.Put(pgbackrestObject.Name)
		for _, repo := range pgbackrestObject.Spec.Configuration.Repositories {
			for _, secret := range CollectSecretNamesFromCredentials(&repo.PgbackrestCredentials) {
				secretsSet.Put(secret)
			}
		}
	}

	role.Rules = append(role.Rules, rbacv1.PolicyRule{
		APIGroups: []string{
			"pgbackrest.cnpg.opera.com",
		},
		Verbs: []string{
			"get",
			"watch",
			"list",
		},
		Resources: []string{
			"archives",
		},
		ResourceNames: pgbackrestObjectsSet.ToSortedList(),
	})

	role.Rules = append(role.Rules, rbacv1.PolicyRule{
		APIGroups: []string{
			"",
		},
		Resources: []string{
			"secrets",
		},
		Verbs: []string{
			"get",
			"watch",
			"list",
		},
		ResourceNames: secretsSet.ToSortedList(),
	})

	return role
}

// BuildRoleBinding builds the role binding object for this cluster
func BuildRoleBinding(
	cluster *cnpgv1.Cluster,
) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      GetRBACName(cluster.Name),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				APIGroup:  "",
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     GetRBACName(cluster.Name),
		},
	}
}

// GetRBACName returns the name of the RBAC entities for the
// pgbackrest plugin
func GetRBACName(clusterName string) string {
	return fmt.Sprintf("%s-pgbackrest", clusterName)
}
