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

package objectstore

import (
	"fmt"
	"net"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/cloudnative-pg/machinery/pkg/api"
	pluginPgbackrestV1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
)

const (
	// azuriteAccount is the Azurite storage account name used for testing.
	azuriteAccount = "storageaccountname"
	// azuriteKey is the Azurite storage account key (base64 encoded) used for testing.
	azuriteKey = "c3RvcmFnZWFjY291bnRrZXk="
	// azuriteContainer is the Azure Blob container (pgBackRest bucket) created for backups.
	azuriteContainer = "backups"

	// AzuriteAccountKey is the secret key holding the Azure storage account name.
	AzuriteAccountKey = "AZURE_STORAGE_ACCOUNT"
	// AzuriteKeyKey is the secret key holding the Azure storage account key.
	AzuriteKeyKey = "AZURE_STORAGE_KEY"
)

// NewAzuriteObjectStoreResources creates the resources required to create an Azurite object store.
func NewAzuriteObjectStoreResources(namespace, name string) *Resources {
	return &Resources{
		Deployment:      newAzuriteDeployment(namespace, name),
		ProvisioningJob: newAzuriteProvisioningJob(namespace, name),
		Service:         newAzuriteService(namespace, name),
		PVC:             newAzuritePVC(namespace, name),
		Secret:          newAzuriteSecret(namespace, name),
	}
}

func newAzuriteDeployment(namespace, name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					// Pgbackrest only allows HTTPS connections to object store endpoints,
					// so Azurite must be started with a self-signed certificate.
					InitContainers: []corev1.Container{
						{
							Name:  "generate-certs",
							Image: "alpine/openssl:latest",
							Args: []string{
								"req",
								"-x509",
								"-newkey",
								"rsa:4096",
								"-keyout",
								"/certs/private.key",
								"-out",
								"/certs/public.crt",
								"-sha256",
								"-days",
								"3650",
								"-nodes",
								"-subj",
								fmt.Sprintf("/CN=%s", name)},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "certs",
									MountPath: "/certs",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: name,
							// TODO: renovate the image
							Image: "mcr.microsoft.com/azure-storage/azurite:latest",
							Args: []string{
								"azurite-blob",
								"--blobHost",
								"0.0.0.0",
								"--location",
								"/data",
								"--skipApiVersionCheck",
								"--disableProductStyleUrl",
								"--cert",
								"/certs/public.crt",
								"--key",
								"/certs/private.key",
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 10000,
									Name:          name,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "AZURITE_ACCOUNTS",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: name,
											},
											Key: "AZURITE_ACCOUNTS",
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/data",
								},
								{
									Name:      "certs",
									MountPath: "/certs",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: name,
								},
							},
						},
						{
							Name: "certs",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}
}

func newAzuriteProvisioningJob(namespace, name string) *batchv1.Job {
	// Pgbackrest requires the Azure Blob container to exist but Azurite doesn't
	// provision it automatically, so we create it with the Azure CLI.
	// The connection string points at the self-signed HTTPS endpoint, hence
	// TLS verification is disabled for the CLI as well.
	connectionString := fmt.Sprintf(
		"DefaultEndpointsProtocol=https;AccountName=%s;AccountKey=%s;BlobEndpoint=https://%s:10000/%s;",
		azuriteAccount, azuriteKey, name, azuriteAccount,
	)
	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-provisioning",
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr.To(int32(10)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:    name + "-provisioner",
							Image:   "mcr.microsoft.com/azure-cli:latest",
							Command: []string{"bash"},
							Args: []string{
								"-c",
								"az storage container create --name " + azuriteContainer +
									" --connection-string \"$AZURE_STORAGE_CONNECTION_STRING\"",
							},
							TerminationMessagePolicy: "FallbackToLogsOnError",
							Env: []corev1.EnvVar{
								{
									Name:  "AZURE_STORAGE_CONNECTION_STRING",
									Value: connectionString,
								},
								{
									Name:  "AZURE_CLI_DISABLE_CONNECTION_VERIFICATION",
									Value: "1",
								},
								{
									Name:  "ADAL_PYTHON_SSL_NO_VERIFY",
									Value: "1",
								},
							},
						},
					},
				},
			},
		},
	}
}

func newAzuriteService(namespace, name string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": name,
			},
			Ports: []corev1.ServicePort{
				{
					Port:       10000,
					TargetPort: intstr.FromInt32(10000),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

func newAzuriteSecret(namespace, name string) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"AZURITE_ACCOUNTS": []byte(fmt.Sprintf("%s:%s", azuriteAccount, azuriteKey)),
			AzuriteAccountKey:  []byte(azuriteAccount),
			AzuriteKeyKey:      []byte(azuriteKey),
		},
	}
}

func newAzuritePVC(namespace, name string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PersistentVolumeClaim",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(DefaultSize),
				},
			},
		},
	}
}

// NewAzuriteArchive creates a new Archive configured to use the Azurite object store.
func NewAzuriteArchive(namespace, name, azuriteOSName string, maxParallel int) *pluginPgbackrestV1.Archive {
	return &pluginPgbackrestV1.Archive{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Archive",
			APIVersion: "pgbackrest.cnpg.opera.com/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: pluginPgbackrestV1.ArchiveSpec{
			Configuration: pgbackrestApi.PgbackrestConfiguration{
				Wal: &pgbackrestApi.WalBackupConfiguration{
					MaxParallel: maxParallel,
				},
				Repositories: []pgbackrestApi.PgbackrestRepository{
					{
						PgbackrestCredentials: pgbackrestApi.PgbackrestCredentials{
							Azure: &pgbackrestApi.AzureCredentials{
								KeyType: pgbackrestApi.AzureKeyTypeShared,
								Account: &api.SecretKeySelector{
									LocalObjectReference: api.LocalObjectReference{
										Name: azuriteOSName,
									},
									Key: AzuriteAccountKey,
								},
								Key: &api.SecretKeySelector{
									LocalObjectReference: api.LocalObjectReference{
										Name: azuriteOSName,
									},
									Key: AzuriteKeyKey,
								},
								// Azurite exposes the account name in the URL path,
								// while pgBackRest defaults to host-style addressing.
								URIStyle: "path",
							},
						},
						EndpointURL: net.JoinHostPort(azuriteOSName, "10000"),
						// Pgbackrest enforces HTTPS connections and there is only
						// a self-signed certificate available.
						DisableVerifyTLS: true,
						DestinationPath:  "/",
						Bucket:           azuriteContainer,
					},
				},
			},
		},
	}
}
