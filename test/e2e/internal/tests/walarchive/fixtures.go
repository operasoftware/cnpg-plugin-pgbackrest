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

package walarchive

import (
	cloudnativepgv1 "github.com/cloudnative-pg/api/pkg/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pluginPgbackrestV1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/objectstore"
)

const (
	minio = "minio"
	// size is the size of the PVCs for the object store and the cluster instances.
	size           = "1Gi"
	pluginName     = "pgbackrest.cnpg.opera.com"
	srcClusterName = "wal-archive-source"
	archiveName    = "wal-archive"
	backupName     = "wal-archive-backup"
)

// walArchiveTestResources contains the resources needed to test WAL archiving,
// with a backup to prove that path still works.
type walArchiveTestResources struct {
	ObjectStoreResources *objectstore.Resources
	Archive              *pluginPgbackrestV1.Archive
	Cluster              *cloudnativepgv1.Cluster
	Backup               *cloudnativepgv1.Backup
}

// createWalArchiveTestResources builds all resources for the WAL archiving test.
func createWalArchiveTestResources(namespace string) walArchiveTestResources {
	return walArchiveTestResources{
		ObjectStoreResources: objectstore.NewMinioObjectStoreResources(namespace, minio),
		// maxParallel=1 so every WAL is archived in its own batch, which keeps the
		// assertions on batch completions unambiguous.
		Archive: objectstore.NewMinioArchive(namespace, archiveName, minio, 1),
		Cluster: newClusterWithPlugin(namespace, srcClusterName),
		Backup:  newPluginBackup(namespace, backupName, srcClusterName),
	}
}

// newClusterWithPlugin creates a cluster that only enables the plugin for WAL
// archiving. Crucially it defines no bootstrap and no backup, so the stanza must
// be created lazily on the first WAL archive.
func newClusterWithPlugin(namespace, name string) *cloudnativepgv1.Cluster {
	return &cloudnativepgv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: cloudnativepgv1.ClusterSpec{
			Instances:       2,
			ImagePullPolicy: corev1.PullAlways,
			Plugins: []cloudnativepgv1.PluginConfiguration{
				{
					Name: pluginName,
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
}

// newPluginBackup creates a plugin backup targeting the primary of the given cluster.
func newPluginBackup(namespace, name, clusterName string) *cloudnativepgv1.Backup {
	return &cloudnativepgv1.Backup{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Backup",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: cloudnativepgv1.BackupSpec{
			Cluster: cloudnativepgv1.LocalObjectReference{
				Name: clusterName,
			},
			Method: "plugin",
			Target: "primary",
			PluginConfiguration: &cloudnativepgv1.BackupPluginConfiguration{
				Name: pluginName,
			},
		},
	}
}
