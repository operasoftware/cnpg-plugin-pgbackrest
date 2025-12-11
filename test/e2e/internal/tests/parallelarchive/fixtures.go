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
	cloudnativepgv1 "github.com/cloudnative-pg/api/pkg/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pluginPgbackrestV1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/objectstore"
)

const (
	minio       = "minio"
	size        = "1Gi"
	clusterName = "parallel-archive-test"
	archiveName = "parallel-archive"
)

// parallelArchiveTestResources contains the resources needed for parallel archive testing
type parallelArchiveTestResources struct {
	ObjectStoreResources *objectstore.Resources
	Archive              *pluginPgbackrestV1.Archive
	Cluster              *cloudnativepgv1.Cluster
}

// createParallelArchiveTestResources creates test resources for parallel archive testing
func createParallelArchiveTestResources(namespace string, maxParallel int) parallelArchiveTestResources {
	resources := parallelArchiveTestResources{}

	resources.ObjectStoreResources = objectstore.NewMinioObjectStoreResources(namespace, minio)
	resources.Archive = objectstore.NewMinioArchive(namespace, archiveName, minio, maxParallel)
	resources.Cluster = createClusterWithArchive(namespace, clusterName, archiveName)

	return resources
}

// createClusterWithArchive creates a CloudNativePG cluster configured to use the specified archive
func createClusterWithArchive(namespace, clusterName, archiveName string) *cloudnativepgv1.Cluster {
	cluster := &cloudnativepgv1.Cluster{
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
			PostgresConfiguration: cloudnativepgv1.PostgresConfiguration{
				Parameters: map[string]string{
					"log_min_messages": "DEBUG4",
				},
			},
			StorageConfiguration: cloudnativepgv1.StorageConfiguration{
				Size: size,
			},
		},
	}

	return cluster
}
