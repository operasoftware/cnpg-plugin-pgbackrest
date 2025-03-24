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

// NewMinioObjectStoreResources creates the resources required to create a Minio object store.
func NewMinioObjectStoreResources(namespace, name string) *Resources {
	return &Resources{
		Deployment:      newMinioDeployment(namespace, name),
		ProvisioningJob: newMinioProvisioningJob(namespace, name),
		Service:         newMinioService(namespace, name),
		PVC:             newMinioPVC(namespace, name),
		Secret:          newMinioSecret(namespace, name),
	}
}

func newMinioDeployment(namespace, name string) *appsv1.Deployment {
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
					// Pgbackrest only allows HTTPS connections to S3 endpoints.
					// That means minio must be configured in HTTPS mode. It's enabled
					// automatically if the ${HOME}/.minio/certs directory contains
					// certificates.
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
								"/root/.minio/certs/private.key",
								"-out",
								"/root/.minio/certs/public.crt",
								"-sha256",
								"-days",
								"3650",
								"-nodes",
								"-subj",
								fmt.Sprintf("/CN=%s", name)},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "certs",
									MountPath: "/root/.minio/certs",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: name,
							// TODO: renovate the image
							Image: "minio/minio:latest",
							Args:  []string{"server", "/data"},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 9000,
									Name:          name,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "MINIO_ROOT_USER",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: name,
											},
											Key: "ACCESS_KEY_ID",
										},
									},
								},
								{
									Name: "MINIO_ROOT_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: name,
											},
											Key: "ACCESS_SECRET_KEY",
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
									MountPath: "/root/.minio/certs",
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

func newMinioProvisioningJob(namespace, name string) *batchv1.Job {
	// Pgbackrest requires buckets to exist but the official image doesn't provide any
	// provisioning support.
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
							Name:    name,
							Image:   "minio/minio:latest",
							Command: []string{"bash"},
							Args: []string{
								"-c",
								fmt.Sprintf("mc alias set local https://%s:9000 $MINIO_ROOT_USER $MINIO_ROOT_PASSWORD --insecure;\n", name) +
									"mc mb --insecure local/backups",
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 9000,
									Name:          name,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "MINIO_ROOT_USER",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: name,
											},
											Key: "ACCESS_KEY_ID",
										},
									},
								},
								{
									Name: "MINIO_ROOT_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: name,
											},
											Key: "ACCESS_SECRET_KEY",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func newMinioService(namespace, name string) *corev1.Service {
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
					Port:       9000,
					TargetPort: intstr.FromInt32(9000),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

func newMinioSecret(namespace, name string) *corev1.Secret {
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
			"ACCESS_KEY_ID":     []byte("minio"),
			"ACCESS_SECRET_KEY": []byte("minio123"),
		},
	}
}

func newMinioPVC(namespace, name string) *corev1.PersistentVolumeClaim {
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

// NewMinioArchive creates a new Archive configured to use the Minio object store.
func NewMinioArchive(namespace, name, minioOSName string) *pluginPgbackrestV1.Archive {
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
				Repositories: []pgbackrestApi.PgbackrestRepository{
					{
						PgbackrestCredentials: pgbackrestApi.PgbackrestCredentials{
							AWS: &pgbackrestApi.S3Credentials{
								AccessKeyIDReference: &api.SecretKeySelector{
									LocalObjectReference: api.LocalObjectReference{
										Name: minioOSName,
									},
									Key: "ACCESS_KEY_ID",
								},
								SecretAccessKeyReference: &api.SecretKeySelector{
									LocalObjectReference: api.LocalObjectReference{
										Name: minioOSName,
									},
									Key: "ACCESS_SECRET_KEY",
								},
								Region: "dummy",
								// There is no ingress that would provide domain-based
								// routing.
								UriStyle: "path",
							},
						},
						EndpointURL: net.JoinHostPort(minioOSName, "9000"),
						// Pgbackrest enforces HTTPS connections and there is only
						// a self-signed certificate available.
						DisableVerifyTLS: true,
						DestinationPath:  "/",
						Bucket:           "backups",
					},
				},
			},
		},
	}
}
