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
	"encoding/json"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cnpg-i/pkg/lifecycle"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/operator/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LifecycleImplementation", func() {
	var (
		lifecycleImpl       LifecycleImplementation
		pluginConfiguration *config.PluginConfiguration
		cluster             *cnpgv1.Cluster
		jobTypeMeta         = metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		}
		podTypeMeta = metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		}
	)

	BeforeEach(func() {
		pluginConfiguration = &config.PluginConfiguration{
			PgbackrestObjectName: "minio-store-dest",
		}
		cluster = &cnpgv1.Cluster{
			Spec: cnpgv1.ClusterSpec{
				Bootstrap: &cnpgv1.BootstrapConfiguration{
					Recovery: &cnpgv1.BootstrapRecovery{
						Source: "origin-server",
					},
				},
				ExternalClusters: []cnpgv1.ExternalCluster{
					{
						Name: "origin-server",
						PluginConfiguration: &cnpgv1.PluginConfiguration{
							Name: "pgbackrest.cnpg.opera.com",
							Parameters: map[string]string{
								"pgbackrestObjectName": "minio-store-source",
							},
						},
					},
				},
				Plugins: []cnpgv1.PluginConfiguration{
					{
						Name: "pgbackrest.cnpg.opera.com",
						Parameters: map[string]string{
							"pgbackrestObjectName": "minio-store-dest",
						},
					},
				},
			},
		}
	})

	Describe("GetCapabilities", func() {
		It("returns the correct capabilities", func(ctx SpecContext) {
			response, err := lifecycleImpl.GetCapabilities(ctx, &lifecycle.OperatorLifecycleCapabilitiesRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(response).NotTo(BeNil())
			Expect(response.LifecycleCapabilities).To(HaveLen(2))
		})
	})

	Describe("LifecycleHook", func() {
		It("returns an error if object definition is invalid", func(ctx SpecContext) {
			request := &lifecycle.OperatorLifecycleRequest{
				ObjectDefinition: []byte("invalid-json"),
			}
			response, err := lifecycleImpl.LifecycleHook(ctx, request)
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
		})
	})

	Describe("reconcileJob", func() {
		It("returns a patch for a valid recovery job", func(ctx SpecContext) {
			job := &batchv1.Job{
				TypeMeta: jobTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-job",
					Labels: map[string]string{},
				},
				Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							utils.JobRoleLabelName: "full-recovery",
						},
					},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "full-recovery"}}},
				}},
			}
			jobJSON, _ := json.Marshal(job)
			request := &lifecycle.OperatorLifecycleRequest{
				ObjectDefinition: jobJSON,
			}

			response, err := reconcileJob(ctx, cluster, request, nil, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(response).NotTo(BeNil())
			Expect(response.JsonPatch).NotTo(BeEmpty())
		})

		It("skips non-recovery jobs", func(ctx SpecContext) {
			job := &batchv1.Job{
				TypeMeta: jobTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-job",
					Labels: map[string]string{
						"job-role": "non-recovery",
					},
				},
			}
			jobJSON, _ := json.Marshal(job)
			request := &lifecycle.OperatorLifecycleRequest{
				ObjectDefinition: jobJSON,
			}

			response, err := reconcileJob(ctx, cluster, request, nil, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(response).To(BeNil())
		})

		It("returns an error for invalid job definition", func(ctx SpecContext) {
			request := &lifecycle.OperatorLifecycleRequest{
				ObjectDefinition: []byte("invalid-json"),
			}

			response, err := reconcileJob(ctx, cluster, request, nil, nil, nil)
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
		})

		It("should not error out if backup object name is not set and the job isn't full recovery",
			func(ctx SpecContext) {
				job := &batchv1.Job{
					TypeMeta: jobTypeMeta,
					ObjectMeta: metav1.ObjectMeta{
						Name:   "test-job",
						Labels: map[string]string{},
					},
					Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								utils.JobRoleLabelName: "non-recovery",
							},
						},
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "non-recovery"}}},
					}},
				}
				jobJSON, _ := json.Marshal(job)
				request := &lifecycle.OperatorLifecycleRequest{
					ObjectDefinition: jobJSON,
				}

				response, err := reconcileJob(ctx, cluster, request, nil, nil, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(response).To(BeNil())
			})
	})

	Describe("reconcilePod", func() {
		It("returns a patch for a valid pod", func(ctx SpecContext) {
			pod := &corev1.Pod{
				TypeMeta: podTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "postgres"}}},
			}
			podJSON, _ := json.Marshal(pod)
			request := &lifecycle.OperatorLifecycleRequest{
				ObjectDefinition: podJSON,
			}

			response, err := reconcilePod(ctx, cluster, request, pluginConfiguration, nil, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(response).NotTo(BeNil())
			Expect(response.JsonPatch).NotTo(BeEmpty())
			var patch []map[string]interface{}
			err = json.Unmarshal(response.JsonPatch, &patch)
			Expect(err).NotTo(HaveOccurred())
			Expect(patch).To(ContainElement(HaveKeyWithValue("op", "add")))
			Expect(patch).To(ContainElement(HaveKeyWithValue("path", "/spec/initContainers")))
			Expect(patch).To(ContainElement(
				HaveKey("value")))
		})

		It("returns an error for invalid pod definition", func(ctx SpecContext) {
			request := &lifecycle.OperatorLifecycleRequest{
				ObjectDefinition: []byte("invalid-json"),
			}

			response, err := reconcilePod(ctx, cluster, request, pluginConfiguration, nil, nil, nil)
			Expect(err).To(HaveOccurred())
			Expect(response).To(BeNil())
		})
	})

	Describe("calculateSidecarSecurityContext", func() {
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
		})

		It("returns hardened default when archive is nil", func() {
			result := lifecycleImpl.calculateSidecarSecurityContext(ctx, nil)
			Expect(result).ToNot(BeNil())
			Expect(result.AllowPrivilegeEscalation).To(Equal(ptr.To(false)))
			Expect(result.RunAsNonRoot).To(Equal(ptr.To(true)))
			Expect(result.ReadOnlyRootFilesystem).To(Equal(ptr.To(true)))
			Expect(result.Privileged).To(Equal(ptr.To(false)))
			Expect(result.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
			Expect(result.Capabilities.Drop).To(Equal([]corev1.Capability{"ALL"}))
		})

		It("returns hardened default when archive has no security context", func() {
			archive := &pgbackrestv1.Archive{
				Spec: pgbackrestv1.ArchiveSpec{
					InstanceSidecarConfiguration: pgbackrestv1.InstanceSidecarConfiguration{
						SecurityContext: nil,
					},
				},
			}

			result := lifecycleImpl.calculateSidecarSecurityContext(ctx, archive)
			Expect(result).ToNot(BeNil())
			Expect(result.AllowPrivilegeEscalation).To(Equal(ptr.To(false)))
			Expect(result.RunAsNonRoot).To(Equal(ptr.To(true)))
			Expect(result.ReadOnlyRootFilesystem).To(Equal(ptr.To(true)))
			Expect(result.Privileged).To(Equal(ptr.To(false)))
			Expect(result.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
			Expect(result.Capabilities.Drop).To(Equal([]corev1.Capability{"ALL"}))
		})

		It("returns configured security context when present", func() {
			expectedSecurityContext := &corev1.SecurityContext{
				AllowPrivilegeEscalation: ptr.To(false),
				ReadOnlyRootFilesystem:   ptr.To(true),
				RunAsNonRoot:             ptr.To(true),
				RunAsUser:                ptr.To(int64(26)),
				RunAsGroup:               ptr.To(int64(26)),
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			}

			archive := &pgbackrestv1.Archive{
				Spec: pgbackrestv1.ArchiveSpec{
					InstanceSidecarConfiguration: pgbackrestv1.InstanceSidecarConfiguration{
						SecurityContext: expectedSecurityContext,
					},
				},
			}

			result := lifecycleImpl.calculateSidecarSecurityContext(ctx, archive)
			Expect(result).To(Equal(expectedSecurityContext))
		})
	})

	Describe("reconcilePod with security context", func() {
		It("applies custom security context to sidecar when configured", func(ctx SpecContext) {
			// Given
			pod := &corev1.Pod{
				TypeMeta: podTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "postgres"}}},
			}
			podJSON, _ := json.Marshal(pod)
			request := &lifecycle.OperatorLifecycleRequest{
				ObjectDefinition: podJSON,
			}

			customSecurityContext := &corev1.SecurityContext{
				AllowPrivilegeEscalation: ptr.To(false),
				ReadOnlyRootFilesystem:   ptr.To(true),
				RunAsNonRoot:             ptr.To(true),
				RunAsUser:                ptr.To(int64(1000)),
				RunAsGroup:               ptr.To(int64(1000)),
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			}

			response, err := reconcilePod(ctx, cluster, request, pluginConfiguration, nil, nil, customSecurityContext)
			Expect(err).NotTo(HaveOccurred())
			Expect(response).NotTo(BeNil())
			Expect(response.JsonPatch).NotTo(BeEmpty())

			var patch []map[string]interface{}
			err = json.Unmarshal(response.JsonPatch, &patch)
			Expect(err).NotTo(HaveOccurred())

			var initContainersPatch map[string]interface{}
			for _, p := range patch {
				if p["path"] == "/spec/initContainers" && p["op"] == "add" {
					initContainersPatch = p
					break
				}
			}
			Expect(initContainersPatch).NotTo(BeNil())

			// Get the init containers patch
			initContainers, ok := initContainersPatch["value"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(initContainers).To(HaveLen(1))

			// Verify the init container contains the security context
			container, ok := initContainers[0].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(container).To(HaveKey("securityContext"))
		})

		It("doesn't set security context when it's nil", func(ctx SpecContext) {
			pod := &corev1.Pod{
				TypeMeta: podTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "postgres"}}},
			}
			podJSON, _ := json.Marshal(pod)
			request := &lifecycle.OperatorLifecycleRequest{
				ObjectDefinition: podJSON,
			}

			response, err := reconcilePod(ctx, cluster, request, pluginConfiguration, nil, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(response).NotTo(BeNil())
			Expect(response.JsonPatch).NotTo(BeEmpty())

			var patch []map[string]interface{}
			err = json.Unmarshal(response.JsonPatch, &patch)
			Expect(err).NotTo(HaveOccurred())

			var initContainersPatch map[string]interface{}
			for _, p := range patch {
				if p["path"] == "/spec/initContainers" && p["op"] == "add" {
					initContainersPatch = p
					break
				}
			}
			Expect(initContainersPatch).NotTo(BeNil())

			// Get the init container patch
			initContainers, ok := initContainersPatch["value"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(initContainers).To(HaveLen(1))

			// Verify the init container doesn't contain the security context
			container, ok := initContainers[0].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(container).NotTo(HaveKey("securityContext"))
		})
	})

	Describe("reconcileJob with security context", func() {
		It("applies custom security context to job sidecar when configured", func(ctx SpecContext) {
			job := &batchv1.Job{
				TypeMeta: jobTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-job",
					Labels: map[string]string{},
				},
				Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							utils.JobRoleLabelName: "full-recovery",
						},
					},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "full-recovery"}}},
				}},
			}
			jobJSON, _ := json.Marshal(job)
			request := &lifecycle.OperatorLifecycleRequest{
				ObjectDefinition: jobJSON,
			}

			customSecurityContext := &corev1.SecurityContext{
				AllowPrivilegeEscalation: ptr.To(false),
				ReadOnlyRootFilesystem:   ptr.To(true),
				RunAsNonRoot:             ptr.To(true),
				RunAsUser:                ptr.To(int64(1000)),
				RunAsGroup:               ptr.To(int64(1000)),
			}

			response, err := reconcileJob(ctx, cluster, request, nil, nil, customSecurityContext)
			Expect(err).NotTo(HaveOccurred())
			Expect(response).NotTo(BeNil())
			Expect(response.JsonPatch).NotTo(BeEmpty())

			var patch []map[string]interface{}
			err = json.Unmarshal(response.JsonPatch, &patch)
			Expect(err).NotTo(HaveOccurred())
			Expect(patch).NotTo(BeEmpty())

			var initContainersPatch map[string]interface{}
			for _, p := range patch {
				if p["path"] == "/spec/template/spec/initContainers" && p["op"] == "add" {
					initContainersPatch = p
					break
				}
			}
			Expect(initContainersPatch).NotTo(BeNil())

			// Get the init containers patch
			initContainers, ok := initContainersPatch["value"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(initContainers).To(HaveLen(1))

			// Verify the init container contains the security context
			container, ok := initContainers[0].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(container).To(HaveKey("securityContext"))
		})

		It("doesn't set security context when it's nil", func(ctx SpecContext) {
			job := &batchv1.Job{
				TypeMeta: jobTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-job",
					Labels: map[string]string{},
				},
				Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							utils.JobRoleLabelName: "full-recovery",
						},
					},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "full-recovery"}}},
				}},
			}
			jobJSON, _ := json.Marshal(job)
			request := &lifecycle.OperatorLifecycleRequest{
				ObjectDefinition: jobJSON,
			}

			response, err := reconcileJob(ctx, cluster, request, nil, nil, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(response).NotTo(BeNil())
			Expect(response.JsonPatch).NotTo(BeEmpty())

			var patch []map[string]interface{}
			err = json.Unmarshal(response.JsonPatch, &patch)
			Expect(err).NotTo(HaveOccurred())
			Expect(patch).NotTo(BeEmpty())

			var initContainersPatch map[string]interface{}
			for _, p := range patch {
				if p["path"] == "/spec/template/spec/initContainers" && p["op"] == "add" {
					initContainersPatch = p
					break
				}
			}
			Expect(initContainersPatch).NotTo(BeNil())

			// Get the init containers patch
			initContainers, ok := initContainersPatch["value"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(initContainers).To(HaveLen(1))

			// Verify the init container doesn't contain the security context
			container, ok := initContainers[0].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(container).NotTo(HaveKey("securityContext"))
		})
	})
})
