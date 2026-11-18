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

package standbybackup

import (
	cloudnativepgv1 "github.com/cloudnative-pg/api/pkg/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	pluginPgbackrestV1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/objectstore"
)

const (
	minio = "minio"
	// Size of the PVCs for the cluster instances.
	size               = "1Gi"
	archiveName        = "source"
	srcClusterName     = "source"
	restoreClusterName = "restore"
	firstBackupName    = "standby-1"
	secondBackupName   = "standby-2"

	// Port of the pgBackRest TLS server run by the plugin sidecars.
	pgbackrestServerPort = 8432
)

func newArchive(namespace string) *pluginPgbackrestV1.Archive {
	archive := objectstore.NewMinioArchive(namespace, archiveName, minio, 1)
	archive.Spec.Configuration.BackupStandby = &pgbackrestApi.BackupStandbyConfiguration{Enabled: true}
	if archive.Spec.Configuration.Data == nil {
		archive.Spec.Configuration.Data = &pgbackrestApi.DataBackupConfiguration{}
	}
	archive.Spec.Configuration.Data.ImmediateCheckpoint = true

	return archive
}

func newSrcCluster(namespace string) *cloudnativepgv1.Cluster {
	return &cloudnativepgv1.Cluster{
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
			// CloudNativePG creates the Service and adds its name to the instance
			// server certificate, which the standby verifies.
			Managed: &cloudnativepgv1.ManagedConfiguration{
				Services: &cloudnativepgv1.ManagedServices{
					Additional: []cloudnativepgv1.ManagedService{
						{
							SelectorType: cloudnativepgv1.ServiceSelectorTypeRW,
							ServiceTemplate: cloudnativepgv1.ServiceTemplateSpec{
								ObjectMeta: cloudnativepgv1.Metadata{
									Name: srcClusterName + "-pgbackrest",
								},
								Spec: corev1.ServiceSpec{
									Ports: []corev1.ServicePort{
										{
											Name:       "pgbackrest",
											Port:       pgbackrestServerPort,
											TargetPort: intstr.FromInt32(pgbackrestServerPort),
										},
									},
								},
							},
						},
					},
				},
			},
			StorageConfiguration: cloudnativepgv1.StorageConfiguration{
				Size: size,
			},
		},
	}
}

func newStandbyBackup(namespace, name string) *cloudnativepgv1.Backup {
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
				Name: srcClusterName,
			},
			Method: "plugin",
			Target: cloudnativepgv1.BackupTargetStandby,
			PluginConfiguration: &cloudnativepgv1.BackupPluginConfiguration{
				Name: "pgbackrest.cnpg.opera.com",
				Parameters: map[string]string{
					"type": "full",
				},
			},
		},
	}
}

// newRestoreCluster recovers the source backups to the end of the archive.
func newRestoreCluster(namespace string) *cloudnativepgv1.Cluster {
	return &cloudnativepgv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "postgresql.cnpg.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreClusterName,
			Namespace: namespace,
		},
		Spec: cloudnativepgv1.ClusterSpec{
			Instances:       1,
			ImagePullPolicy: corev1.PullAlways,
			Bootstrap: &cloudnativepgv1.BootstrapConfiguration{
				Recovery: &cloudnativepgv1.BootstrapRecovery{
					Source: "source",
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
}
