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

package backup

import (
	cloudnativepgv1 "github.com/cloudnative-pg/api/pkg/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pluginPgbackrestV1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/objectstore"
)

const (
	minio = "minio"
	// Size of the PVCs for the object stores and the cluster instances.
	size               = "1Gi"
	srcClusterName     = "source"
	srcBackupName      = "source"
	archiveName        = "source"
	dstBackupName      = "restore"
	restoreClusterName = "restore"
	pitrClusterName    = "pitr-restore"
)

type testCaseFactory interface {
	createBackupRestoreTestResources(namespace string) backupRestoreTestResources
}

type pitrTestCaseFactory interface {
	testCaseFactory
	createPITRCluster(namespace string, targetTime string) *cloudnativepgv1.Cluster
}

type backupRestoreTestResources struct {
	ObjectStoreResources *objectstore.Resources
	Archive              *pluginPgbackrestV1.Archive
	SrcCluster           *cloudnativepgv1.Cluster
	SrcBackup            *cloudnativepgv1.Backup
	DstCluster           *cloudnativepgv1.Cluster
	DstBackup            *cloudnativepgv1.Backup
}

type s3BackupPluginBackupPluginRestore struct{}

type s3BackupPluginTargetTimeRestore struct {
	s3BackupPluginBackupPluginRestore
}

func (s s3BackupPluginBackupPluginRestore) createBackupRestoreTestResources(
	namespace string,
) backupRestoreTestResources {
	result := backupRestoreTestResources{}

	result.ObjectStoreResources = objectstore.NewMinioObjectStoreResources(namespace, minio)
	result.Archive = objectstore.NewMinioArchive(namespace, archiveName, minio)
	result.SrcCluster = newSrcClusterWithPlugin(namespace)
	result.SrcBackup = newSrcPluginBackup(namespace)
	result.DstCluster = newDstClusterWithPlugin(namespace)
	result.DstBackup = newDstPluginBackup(namespace)

	return result
}

func (s s3BackupPluginTargetTimeRestore) createPITRCluster(
	namespace string,
	targetTime string,
) *cloudnativepgv1.Cluster {
	cluster := &cloudnativepgv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pitrClusterName,
			Namespace: namespace,
		},
		Spec: cloudnativepgv1.ClusterSpec{
			Instances:       2,
			ImagePullPolicy: corev1.PullAlways,
			Bootstrap: &cloudnativepgv1.BootstrapConfiguration{
				Recovery: &cloudnativepgv1.BootstrapRecovery{
					Source: "source",
					RecoveryTarget: &cloudnativepgv1.RecoveryTarget{
						TargetTime: targetTime,
					},
				},
			},
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
			ExternalClusters: []cloudnativepgv1.ExternalCluster{
				{
					Name: "source",
					PluginConfiguration: &cloudnativepgv1.PluginConfiguration{
						Name: "pgbackrest.cnpg.opera.com",
						Parameters: map[string]string{
							"pgbackrestObjectName": archiveName,
							"stanza":               srcClusterName,
						},
					},
				},
			},
			StorageConfiguration: cloudnativepgv1.StorageConfiguration{
				Size: size,
			},
		},
	}

	return cluster
}

func newSrcPluginBackup(namespace string) *cloudnativepgv1.Backup {
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
			// TODO: Implement support for standby backups.
			Target: "primary",
			PluginConfiguration: &cloudnativepgv1.BackupPluginConfiguration{
				Name: "pgbackrest.cnpg.opera.com",
			},
		},
	}
}

func newDstPluginBackup(namespace string) *cloudnativepgv1.Backup {
	return &cloudnativepgv1.Backup{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Backup",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      dstBackupName,
			Namespace: namespace,
		},
		Spec: cloudnativepgv1.BackupSpec{
			Cluster: cloudnativepgv1.LocalObjectReference{
				Name: restoreClusterName,
			},
			// TODO: Implement support for standby backups.
			Target: "primary",
			Method: "plugin",
			PluginConfiguration: &cloudnativepgv1.BackupPluginConfiguration{
				Name: "pgbackrest.cnpg.opera.com",
			},
		},
	}
}

func newSrcClusterWithPlugin(namespace string) *cloudnativepgv1.Cluster {
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

func newDstClusterWithPlugin(namespace string) *cloudnativepgv1.Cluster {
	cluster := &cloudnativepgv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreClusterName,
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
						"pgbackrestObjectName": archiveName,
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
							"pgbackrestObjectName": archiveName,
							"stanza":               srcClusterName,
						},
					},
				},
			},
			StorageConfiguration: cloudnativepgv1.StorageConfiguration{
				Size: size,
			},
		},
	}

	return cluster
}
