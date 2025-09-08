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
	"errors"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cnpg-i-machinery/pkg/pluginhelper/decoder"
	"github.com/cloudnative-pg/cnpg-i-machinery/pkg/pluginhelper/object"
	"github.com/cloudnative-pg/cnpg-i/pkg/lifecycle"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/viper"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/operator/config"
)

// LifecycleImplementation is the implementation of the lifecycle handler
type LifecycleImplementation struct {
	lifecycle.UnimplementedOperatorLifecycleServer
	Client client.Client
}

// GetCapabilities exposes the lifecycle capabilities
func (impl LifecycleImplementation) GetCapabilities(
	_ context.Context,
	_ *lifecycle.OperatorLifecycleCapabilitiesRequest,
) (*lifecycle.OperatorLifecycleCapabilitiesResponse, error) {
	return &lifecycle.OperatorLifecycleCapabilitiesResponse{
		LifecycleCapabilities: []*lifecycle.OperatorLifecycleCapabilities{
			{
				Group: "",
				Kind:  "Pod",
				OperationTypes: []*lifecycle.OperatorOperationType{
					{
						Type: lifecycle.OperatorOperationType_TYPE_CREATE,
					},
					{
						Type: lifecycle.OperatorOperationType_TYPE_PATCH,
					},
				},
			},
			{
				Group: batchv1.GroupName,
				Kind:  "Job",
				OperationTypes: []*lifecycle.OperatorOperationType{
					{
						Type: lifecycle.OperatorOperationType_TYPE_CREATE,
					},
				},
			},
		},
	}, nil
}

// LifecycleHook is called when creating Kubernetes services
func (impl LifecycleImplementation) LifecycleHook(
	ctx context.Context,
	request *lifecycle.OperatorLifecycleRequest,
) (*lifecycle.OperatorLifecycleResponse, error) {
	contextLogger := log.FromContext(ctx).WithName("lifecycle")
	contextLogger.Info("Lifecycle hook reconciliation start")
	operation := request.GetOperationType().GetType().Enum()
	if operation == nil {
		return nil, errors.New("no operation set")
	}

	kind, err := object.GetKind(request.GetObjectDefinition())
	if err != nil {
		return nil, err
	}

	var cluster cnpgv1.Cluster
	if err := decoder.DecodeObject(
		request.GetClusterDefinition(),
		&cluster,
		cnpgv1.GroupVersion.WithKind("Cluster"),
	); err != nil {
		return nil, err
	}

	pluginConfiguration := config.NewFromCluster(&cluster)

	// archive object is required for both the archive and restore process
	if err := pluginConfiguration.Validate(); err != nil {
		contextLogger.Info("pluginConfiguration invalid, skipping lifecycle", "error", err)
		return nil, nil
	}

	switch kind {
	case "Pod":
		contextLogger.Info("Reconciling pod")
		return impl.reconcilePod(ctx, &cluster, request, pluginConfiguration)
	case "Job":
		contextLogger.Info("Reconciling job")
		return impl.reconcileJob(ctx, &cluster, request, pluginConfiguration)
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
}

func (impl LifecycleImplementation) calculateSidecarResources(
	ctx context.Context,
	archive *pgbackrestv1.Archive,
) *corev1.ResourceRequirements {
	contextLogger := log.FromContext(ctx).WithName("lifecycle")

	if archive != nil {
		contextLogger.Info("Loading sidecar resources definition from the archive object.")
		return &archive.Spec.InstanceSidecarConfiguration.Resources
	}
	contextLogger.Info("Resources definition not found in the archive object.")
	return nil
}

func (impl LifecycleImplementation) calculateSidecarSecurityContext(
	ctx context.Context,
	archive *pgbackrestv1.Archive,
) *corev1.SecurityContext {
	contextLogger := log.FromContext(ctx).WithName("lifecycle")

	if archive != nil && archive.Spec.InstanceSidecarConfiguration.SecurityContext != nil {
		contextLogger.Info("Loading sidecar security context definition from the archive object.")
		return archive.Spec.InstanceSidecarConfiguration.SecurityContext
	}

	contextLogger.Info("Security context definition not found in the archive object, using default (no restrictions).")
	return nil
}

func (impl LifecycleImplementation) getArchives(
	ctx context.Context,
	namespace string,
	pluginConfiguration *config.PluginConfiguration,
) (*pgbackrestv1.Archive, *pgbackrestv1.Archive, error) {
	var archive pgbackrestv1.Archive
	var recoveryArchive pgbackrestv1.Archive
	contextLogger := log.FromContext(ctx).WithName("lifecycle")
	if len(pluginConfiguration.PgbackrestObjectName) > 0 {
		if err := impl.Client.Get(ctx, types.NamespacedName{
			Name:      pluginConfiguration.PgbackrestObjectName,
			Namespace: namespace,
		}, &archive); err != nil {
			contextLogger.Error(err, "failed to retrieve archive", "error", err)
			return nil, nil, err
		}
	}
	if len(pluginConfiguration.RecoveryPgbackrestObjectName) > 0 {
		if err := impl.Client.Get(ctx, types.NamespacedName{
			Name:      pluginConfiguration.RecoveryPgbackrestObjectName,
			Namespace: namespace,
		}, &recoveryArchive); err != nil {
			contextLogger.Error(err, "failed to retrieve recovery archive", "error", err)
			return nil, nil, err
		}
	}
	return &archive, &recoveryArchive, nil
}

func (impl LifecycleImplementation) collectAdditionalEnvs(
	ctx context.Context,
	archive *pgbackrestv1.Archive,
	recoveryArchive *pgbackrestv1.Archive,
) ([]corev1.EnvVar, error) {
	var result []corev1.EnvVar
	contextLogger := log.FromContext(ctx).WithName("lifecycle")

	if archive != nil {
		envs, err := impl.collectArchiveEnvs(
			ctx,
			archive,
		)
		if err != nil {
			contextLogger.Error(err, "failed to collect env variables from archives", err)
			return nil, err
		}
		result = append(result, envs...)
	}

	if recoveryArchive != nil {
		envs, err := impl.collectArchiveEnvs(
			ctx,
			recoveryArchive,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, envs...)
	}

	return result, nil
}

func (impl LifecycleImplementation) collectArchiveEnvs(
	_ context.Context,
	archive *pgbackrestv1.Archive,
) ([]corev1.EnvVar, error) { // nolint: unparam
	return archive.Spec.InstanceSidecarConfiguration.Env, nil
}

func (impl LifecycleImplementation) reconcileJob(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
	request *lifecycle.OperatorLifecycleRequest,
	pluginConfiguration *config.PluginConfiguration,
) (*lifecycle.OperatorLifecycleResponse, error) {
	archive, recoveryArchive, err := impl.getArchives(ctx, cluster.Namespace, pluginConfiguration)
	if err != nil {
		return nil, err
	}
	env, err := impl.collectAdditionalEnvs(ctx, archive, recoveryArchive)
	if err != nil {
		return nil, err
	}
	resources := impl.calculateSidecarResources(ctx, recoveryArchive)
	securityContext := impl.calculateSidecarSecurityContext(ctx, recoveryArchive)

	return reconcileJob(ctx, cluster, request, env, resources, securityContext)
}

func reconcileJob(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
	request *lifecycle.OperatorLifecycleRequest,
	env []corev1.EnvVar,
	resources *corev1.ResourceRequirements,
	securityContext *corev1.SecurityContext,
) (*lifecycle.OperatorLifecycleResponse, error) {
	contextLogger := log.FromContext(ctx).WithName("lifecycle")
	if pluginConfig := cluster.GetRecoverySourcePlugin(); pluginConfig == nil || pluginConfig.Name != metadata.PluginName {
		contextLogger.Debug("cluster does not use the this plugin for recovery, skipping")
		return nil, nil
	}

	var job batchv1.Job
	if err := decoder.DecodeObject(
		request.GetObjectDefinition(),
		&job,
		batchv1.SchemeGroupVersion.WithKind("Job"),
	); err != nil {
		contextLogger.Error(err, "failed to decode job")
		return nil, err
	}

	contextLogger = log.FromContext(ctx).WithName("plugin-pgbackrest-lifecycle").
		WithValues("jobName", job.Name)
	contextLogger.Debug("starting job reconciliation")

	if job.Spec.Template.Labels[utils.JobRoleLabelName] != "full-recovery" {
		contextLogger.Debug("job is not a recovery job, skipping")
		return nil, nil
	}

	mutatedJob := job.DeepCopy()

	if err := reconcilePodSpec(
		cluster,
		&mutatedJob.Spec.Template.Spec,
		"full-recovery",
		corev1.Container{
			Args: []string{"restore"},
		},
		env, resources, securityContext,
	); err != nil {
		return nil, fmt.Errorf("while reconciling pod spec for job: %w", err)
	}

	patch, err := object.CreatePatch(mutatedJob, &job)
	if err != nil {
		return nil, err
	}

	contextLogger.Debug("generated patch", "content", string(patch))
	return &lifecycle.OperatorLifecycleResponse{
		JsonPatch: patch,
	}, nil
}

func (impl LifecycleImplementation) reconcilePod(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
	request *lifecycle.OperatorLifecycleRequest,
	pluginConfiguration *config.PluginConfiguration,
) (*lifecycle.OperatorLifecycleResponse, error) {
	archive, recoveryArchive, err := impl.getArchives(ctx, cluster.Namespace, pluginConfiguration)
	if err != nil {
		return nil, err
	}
	env, err := impl.collectAdditionalEnvs(ctx, archive, recoveryArchive)
	if err != nil {
		return nil, err
	}
	resources := impl.calculateSidecarResources(ctx, archive)
	securityContext := impl.calculateSidecarSecurityContext(ctx, archive)

	return reconcilePod(ctx, cluster, request, pluginConfiguration, env, resources, securityContext)
}

func reconcilePod(
	ctx context.Context,
	cluster *cnpgv1.Cluster,
	request *lifecycle.OperatorLifecycleRequest,
	pluginConfiguration *config.PluginConfiguration,
	env []corev1.EnvVar,
	resources *corev1.ResourceRequirements,
	securityContext *corev1.SecurityContext,
) (*lifecycle.OperatorLifecycleResponse, error) {
	pod, err := decoder.DecodePodJSON(request.GetObjectDefinition())
	if err != nil {
		return nil, err
	}

	contextLogger := log.FromContext(ctx).WithName("plugin-pgbackrest-lifecycle").
		WithValues("podName", pod.Name)

	mutatedPod := pod.DeepCopy()

	if len(pluginConfiguration.PgbackrestObjectName) != 0 {
		if err := reconcilePodSpec(
			cluster,
			&mutatedPod.Spec,
			"postgres",
			corev1.Container{
				Args: []string{"instance"},
			},
			env, resources, securityContext,
		); err != nil {
			return nil, fmt.Errorf("while reconciling pod spec for pod: %w", err)
		}
	} else {
		contextLogger.Debug("No need to mutate instance with no backup & archiving configuration")
	}

	patch, err := object.CreatePatch(mutatedPod, pod)
	if err != nil {
		return nil, err
	}

	contextLogger.Debug("generated patch", "content", string(patch))
	return &lifecycle.OperatorLifecycleResponse{
		JsonPatch: patch,
	}, nil
}

func reconcilePodSpec(
	cluster *cnpgv1.Cluster,
	spec *corev1.PodSpec,
	mainContainerName string,
	sidecarConfig corev1.Container,
	additionalEnvs []corev1.EnvVar,
	resources *corev1.ResourceRequirements,
	securityContext *corev1.SecurityContext,
) error {
	envs := []corev1.EnvVar{
		{
			Name:  "NAMESPACE",
			Value: cluster.Namespace,
		},
		{
			Name:  "CLUSTER_NAME",
			Value: cluster.Name,
		},
		{
			// TODO: should we really use this one?
			// should we mount an emptyDir volume just for that?
			Name:  "SPOOL_DIRECTORY",
			Value: "/controller/wal-restore-spool",
		},
	}

	envs = append(envs, additionalEnvs...)

	baseProbe := &corev1.Probe{
		FailureThreshold: 10,
		TimeoutSeconds:   10,
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"/manager", "healthcheck", "unix"},
			},
		},
	}

	// fixed values
	sidecarConfig.Name = "plugin-pgbackrest"
	sidecarConfig.Image = viper.GetString("sidecar-image")
	sidecarConfig.ImagePullPolicy = cluster.Spec.ImagePullPolicy
	sidecarConfig.StartupProbe = baseProbe.DeepCopy()

	// merge the main container envs if they aren't already set
	for _, container := range spec.Containers {
		if container.Name == mainContainerName {
			for _, env := range container.Env {
				found := false
				for _, existingEnv := range sidecarConfig.Env {
					if existingEnv.Name == env.Name {
						found = true
						break
					}
				}
				if !found {
					sidecarConfig.Env = append(sidecarConfig.Env, env)
				}
			}
			break
		}
	}

	// merge the default envs if they aren't already set
	for _, env := range envs {
		found := false
		for _, existingEnv := range sidecarConfig.Env {
			if existingEnv.Name == env.Name {
				found = true
				break
			}
		}
		if !found {
			sidecarConfig.Env = append(sidecarConfig.Env, env)
		}
	}

	if resources != nil {
		sidecarConfig.Resources = *resources
	}

	if securityContext != nil {
		sidecarConfig.SecurityContext = securityContext
	}

	if err := InjectPluginSidecarPodSpec(spec, &sidecarConfig, mainContainerName, true); err != nil {
		return err
	}

	return nil
}

// TODO: move to machinery once the logic is finalized

// InjectPluginVolumePodSpec injects the plugin volume into a CNPG Pod spec.
func InjectPluginVolumePodSpec(spec *corev1.PodSpec, mainContainerName string) {
	const (
		pluginVolumeName = "plugins"
		pluginMountPath  = "/plugins"
	)

	foundPluginVolume := false
	for i := range spec.Volumes {
		if spec.Volumes[i].Name == pluginVolumeName {
			foundPluginVolume = true
		}
	}

	if foundPluginVolume {
		return
	}

	spec.Volumes = append(spec.Volumes, corev1.Volume{
		Name: pluginVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	for i := range spec.Containers {
		if spec.Containers[i].Name == mainContainerName {
			spec.Containers[i].VolumeMounts = append(
				spec.Containers[i].VolumeMounts,
				corev1.VolumeMount{
					Name:      pluginVolumeName,
					MountPath: pluginMountPath,
				},
			)
		}
	}
}

// InjectPluginSidecarPodSpec injects a plugin sidecar into a CNPG Pod spec.
//
// If the "injectMainContainerVolumes" flag is true, this will append all the volume
// mounts that are used in the instance manager Pod to the passed sidecar
// container, granting it superuser access to the PostgreSQL instance.
func InjectPluginSidecarPodSpec(
	spec *corev1.PodSpec,
	sidecar *corev1.Container,
	mainContainerName string,
	injectMainContainerVolumes bool,
) error {
	sidecar = sidecar.DeepCopy()
	InjectPluginVolumePodSpec(spec, mainContainerName)

	var volumeMounts []corev1.VolumeMount
	sidecarContainerFound := false
	mainContainerFound := false
	for i := range spec.Containers {
		if spec.Containers[i].Name == mainContainerName {
			volumeMounts = spec.Containers[i].VolumeMounts
			mainContainerFound = true
		}
	}

	if !mainContainerFound {
		return errors.New("main container not found")
	}

	for i := range spec.InitContainers {
		if spec.InitContainers[i].Name == sidecar.Name {
			sidecarContainerFound = true
		}
	}

	if sidecarContainerFound {
		// The sidecar container was already added
		return nil
	}

	// Do not modify the passed sidecar definition
	if injectMainContainerVolumes {
		sidecar.VolumeMounts = append(sidecar.VolumeMounts, volumeMounts...)
	}
	sidecar.RestartPolicy = ptr.To(corev1.ContainerRestartPolicyAlways)
	spec.InitContainers = append(spec.InitContainers, *sidecar)

	return nil
}
