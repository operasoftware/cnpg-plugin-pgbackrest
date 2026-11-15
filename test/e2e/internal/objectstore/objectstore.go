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
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DefaultSize is the default size of the PVCs for the object stores.
	DefaultSize = "1Gi"
)

// Resources represents the resources required to create an object store.
type Resources struct {
	Deployment      *appsv1.Deployment
	ProvisioningJob *batchv1.Job
	Service         *corev1.Service
	Secret          *corev1.Secret
	PVC             *corev1.PersistentVolumeClaim
}

// Create creates the object store resources.
func (osr Resources) Create(ctx context.Context, cl client.Client) error {
	if osr.PVC != nil {
		if err := cl.Create(ctx, osr.PVC); err != nil {
			return fmt.Errorf("failed to create PVC: %w", err)
		}
	}
	if osr.Secret != nil {
		if err := cl.Create(ctx, osr.Secret); err != nil {
			return fmt.Errorf("failed to create secret: %w", err)
		}
	}
	if osr.Deployment != nil {
		if err := cl.Create(ctx, osr.Deployment); err != nil {
			return fmt.Errorf("failed to create deployment: %w", err)
		}
	}
	if osr.Service != nil {
		if err := cl.Create(ctx, osr.Service); err != nil {
			return fmt.Errorf("failed to create service: %w", err)
		}
	}
	if osr.Deployment != nil {
		if err := waitForDeploymentAvailable(ctx, cl, osr.Deployment); err != nil {
			return fmt.Errorf("failed waiting for deployment: %w", err)
		}
	}
	if osr.ProvisioningJob != nil {
		if err := cl.Create(ctx, osr.ProvisioningJob); err != nil {
			return fmt.Errorf("failed to create provisioning job: %w", err)
		}
		if err := waitForJobComplete(ctx, cl, osr.ProvisioningJob); err != nil {
			return fmt.Errorf("failed waiting for provisioning job: %w", err)
		}
	}

	return nil
}

func waitForDeploymentAvailable(ctx context.Context, cl client.Client, deployment *appsv1.Deployment) error {
	key := types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 10*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			current := &appsv1.Deployment{}
			if err := cl.Get(ctx, key, current); err != nil {
				return false, err
			}

			return current.Status.AvailableReplicas >= 1, nil
		})
}

func waitForJobComplete(ctx context.Context, cl client.Client, job *batchv1.Job) error {
	key := types.NamespacedName{Name: job.Name, Namespace: job.Namespace}
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			current := &batchv1.Job{}
			if err := cl.Get(ctx, key, current); err != nil {
				return false, err
			}
			for _, condition := range current.Status.Conditions {
				if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
					return false, fmt.Errorf("job %s failed", key)
				}
				if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
					return true, nil
				}
			}

			return false, nil
		})
}
