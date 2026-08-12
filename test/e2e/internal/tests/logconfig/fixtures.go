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

package logconfig

import (
	cloudnativepgv1 "github.com/cloudnative-pg/api/pkg/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pluginPgbackrestV1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/objectstore"
)

const (
	minio       = "minio"
	size        = "1Gi"
	clusterName = "logcfg"
	backupName  = "logcfg"
	archiveName = "logcfg"

	pluginContainer   = "plugin-pgbackrest"
	postgresContainer = "postgres"

	// The configured stderr log level under test.
	stderrLevel = "debug"
)

type logConfigTestResources struct {
	ObjectStoreResources *objectstore.Resources
	Archive              *pluginPgbackrestV1.Archive
	Cluster              *cloudnativepgv1.Cluster
	Backup               *cloudnativepgv1.Backup
}

func createLogConfigTestResources(namespace string) logConfigTestResources {
	archive := objectstore.NewMinioArchive(namespace, archiveName, minio, 1)
	archive.Spec.Configuration.Log = &pgbackrestApi.LogConfiguration{
		LevelStderr: stderrLevel,
	}

	return logConfigTestResources{
		ObjectStoreResources: objectstore.NewMinioObjectStoreResources(namespace, minio),
		Archive:              archive,
		Cluster:              newClusterWithPlugin(namespace),
		Backup:               newPluginBackup(namespace),
	}
}

func newClusterWithPlugin(namespace string) *cloudnativepgv1.Cluster {
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
				Size: size,
			},
		},
	}
}

func newPluginBackup(namespace string) *cloudnativepgv1.Backup {
	return &cloudnativepgv1.Backup{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Backup",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      backupName,
			Namespace: namespace,
		},
		Spec: cloudnativepgv1.BackupSpec{
			Cluster: cloudnativepgv1.LocalObjectReference{
				Name: clusterName,
			},
			Method: "plugin",
			Target: "primary",
			PluginConfiguration: &cloudnativepgv1.BackupPluginConfiguration{
				Name: "pgbackrest.cnpg.opera.com",
			},
		},
	}
}
