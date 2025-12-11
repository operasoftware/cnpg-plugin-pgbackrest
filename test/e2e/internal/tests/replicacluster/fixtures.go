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

package replicacluster

import (
	cloudnativepgv1 "github.com/cloudnative-pg/api/pkg/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pluginPgbackrestV1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/objectstore"
)

type testCaseFactory interface {
	createReplicaClusterTestResources(namespace string) replicaClusterTestResources
}

const (
	// Size of the PVCs for the object stores and the cluster instances.
	size               = "1Gi"
	srcArchiveName     = "source"
	srcClusterName     = "source"
	srcBackupName      = "source"
	replicaArchiveName = "replica"
	replicaClusterName = "replica"
	replicaBackupName  = "replica"
	minioSrc           = "minio-src"
	minioReplica       = "minio-replica"
)

type replicaClusterTestResources struct {
	SrcObjectStoreResources     *objectstore.Resources
	SrcArchive                  *pluginPgbackrestV1.Archive
	SrcCluster                  *cloudnativepgv1.Cluster
	SrcBackup                   *cloudnativepgv1.Backup
	ReplicaObjectStoreResources *objectstore.Resources
	ReplicaArchive              *pluginPgbackrestV1.Archive
	ReplicaCluster              *cloudnativepgv1.Cluster
	ReplicaBackup               *cloudnativepgv1.Backup
}

type s3ReplicaClusterFactory struct{}

func (f s3ReplicaClusterFactory) createReplicaClusterTestResources(namespace string) replicaClusterTestResources {
	result := replicaClusterTestResources{}

	result.SrcObjectStoreResources = objectstore.NewMinioObjectStoreResources(namespace, minioSrc)
	result.SrcArchive = objectstore.NewMinioArchive(namespace, srcArchiveName, minioSrc, 1)
	result.SrcCluster = newSrcCluster(namespace)
	result.SrcBackup = newSrcBackup(namespace)
	result.ReplicaObjectStoreResources = objectstore.NewMinioObjectStoreResources(namespace, minioReplica)
	result.ReplicaArchive = objectstore.NewMinioArchive(namespace, replicaArchiveName, minioReplica, 1)
	result.ReplicaCluster = newReplicaCluster(namespace)
	result.ReplicaBackup = newReplicaBackup(namespace)

	return result
}

func newSrcCluster(namespace string) *cloudnativepgv1.Cluster {
	cluster := &cloudnativepgv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      srcClusterName,
			Namespace: namespace,
		},
		Spec: cloudnativepgv1.ClusterSpec{
			Instances:       2,
			ImagePullPolicy: corev1.PullAlways,
			Plugins: []cloudnativepgv1.PluginConfiguration{
				{
					Name: "pgbackrest.cnpg.opera.com",
					Parameters: map[string]string{
						"pgbackrestObjectName": srcArchiveName,
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
			ReplicaCluster: &cloudnativepgv1.ReplicaClusterConfiguration{
				Primary: "source",
				Source:  "replica",
			},
			ExternalClusters: []cloudnativepgv1.ExternalCluster{
				{
					Name: "source",
					PluginConfiguration: &cloudnativepgv1.PluginConfiguration{
						Name: "pgbackrest.cnpg.opera.com",
						Parameters: map[string]string{
							"pgbackrestObjectName": srcArchiveName,
							"stanza":               srcClusterName,
						},
					},
				},
				{
					Name: "replica",
					PluginConfiguration: &cloudnativepgv1.PluginConfiguration{
						Name: "pgbackrest.cnpg.opera.com",
						Parameters: map[string]string{
							"pgbackrestObjectName": replicaArchiveName,
							"stanza":               replicaArchiveName,
						},
					},
				},
			},
		},
	}

	return cluster
}

func newSrcBackup(namespace string) *cloudnativepgv1.Backup {
	return &cloudnativepgv1.Backup{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Backup",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      srcBackupName,
			Namespace: namespace,
		},
		Spec: cloudnativepgv1.BackupSpec{
			Cluster: cloudnativepgv1.LocalObjectReference{
				Name: srcClusterName,
			},
			Method: "plugin",
			PluginConfiguration: &cloudnativepgv1.BackupPluginConfiguration{
				Name: "pgbackrest.cnpg.opera.com",
			},
			Target: "primary",
		},
	}
}

func newReplicaBackup(namespace string) *cloudnativepgv1.Backup {
	return &cloudnativepgv1.Backup{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Backup",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      replicaBackupName,
			Namespace: namespace,
		},
		Spec: cloudnativepgv1.BackupSpec{
			Cluster: cloudnativepgv1.LocalObjectReference{
				Name: replicaClusterName,
			},
			Method: "plugin",
			PluginConfiguration: &cloudnativepgv1.BackupPluginConfiguration{
				Name: "pgbackrest.cnpg.opera.com",
			},
			Target: "primary",
		},
	}
}

func newReplicaCluster(namespace string) *cloudnativepgv1.Cluster {
	cluster := &cloudnativepgv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      replicaClusterName,
			Namespace: namespace,
		},
		Spec: cloudnativepgv1.ClusterSpec{
			Instances:       2,
			ImagePullPolicy: corev1.PullAlways,
			Bootstrap: &cloudnativepgv1.BootstrapConfiguration{
				Recovery: &cloudnativepgv1.BootstrapRecovery{
					Source: "source",
				},
			},
			Plugins: []cloudnativepgv1.PluginConfiguration{
				{
					Name: "pgbackrest.cnpg.opera.com",
					Parameters: map[string]string{
						"pgbackrestObjectName": replicaArchiveName,
					},
				},
			},
			PostgresConfiguration: cloudnativepgv1.PostgresConfiguration{
				Parameters: map[string]string{
					"log_min_messages": "DEBUG4",
				},
			},
			ExternalClusters: []cloudnativepgv1.ExternalCluster{
				{
					Name: "source",
					PluginConfiguration: &cloudnativepgv1.PluginConfiguration{
						Name: "pgbackrest.cnpg.opera.com",
						Parameters: map[string]string{
							"pgbackrestObjectName": srcArchiveName,
							"stanza":               srcClusterName,
						},
					},
				},
				{
					Name: "replica",
					PluginConfiguration: &cloudnativepgv1.PluginConfiguration{
						Name: "pgbackrest.cnpg.opera.com",
						Parameters: map[string]string{
							"pgbackrestObjectName": replicaArchiveName,
							"stanza":               replicaArchiveName,
						},
					},
				},
			},
			ReplicaCluster: &cloudnativepgv1.ReplicaClusterConfiguration{
				Primary: "source",
				Source:  "source",
			},
			StorageConfiguration: cloudnativepgv1.StorageConfiguration{
				Size: size,
			},
		},
	}

	return cluster
}
