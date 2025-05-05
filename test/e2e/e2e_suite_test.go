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

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	apimachineryTypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	kustomizeTypes "sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/resid"

	internalClient "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/client"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/deployment"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/e2etestenv"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/kustomize"

	_ "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/tests/backup"
	_ "github.com/operasoftware/cnpg-plugin-pgbackrest/test/e2e/internal/tests/replicacluster"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// We don't want multiple ginkgo nodes to run the setup concurrently, we use a single cluster for all tests.
var _ = SynchronizedBeforeSuite(func(ctx SpecContext) []byte {
	cl, _, err := internalClient.NewClient()
	if err != nil {
		Fail(fmt.Sprintf("failed to create Kubernetes client: %v", err))
	}

	if err = e2etestenv.Setup(ctx, cl); err != nil {
		Fail(fmt.Sprintf("failed to setup environment: %v", err))
	}

	const pgbackrestKustomizationPath = "./kustomize/kubernetes/"
	pgbackrestKustomization := &kustomizeTypes.Kustomization{
		Resources: []string{pgbackrestKustomizationPath},
		Images: []kustomizeTypes.Image{
			{
				Name:    "operasoftware/cnpg-plugin-pgbackrest-testing",
				NewName: "registry.pgbackrest-plugin:5000/plugin-pgbackrest",
				NewTag:  "testing",
			},
		},
		SecretGenerator: []kustomizeTypes.SecretArgs{
			{
				GeneratorArgs: kustomizeTypes.GeneratorArgs{
					Name:     "plugin-pgbackrest",
					Behavior: "replace",
					KvPairSources: kustomizeTypes.KvPairSources{
						LiteralSources: []string{"SIDECAR_IMAGE=registry.pgbackrest-plugin:5000/sidecar-pgbackrest:testing"},
					},
				},
			},
		},
		Patches: []kustomizeTypes.Patch{
			{
				Patch: `[{"op": "replace", "path": "/spec/template/spec/containers/0/imagePullPolicy", "value": "Always"}]`,
				Target: &kustomizeTypes.Selector{
					ResId: resid.ResId{
						Gvk: resid.Gvk{
							Group:   "apps",
							Version: "v1",
							Kind:    "Deployment",
						},
						Name:      "pgbackrest",
						Namespace: "cnpg-system",
					},
				},
				Options: nil,
			},
		},
	}

	if err := kustomize.ApplyKustomization(ctx, cl, pgbackrestKustomization); err != nil {
		Fail(fmt.Sprintf("failed to apply kustomization: %v", err))
	}
	const defaultTimeout = 1 * time.Minute
	ctxDeploy, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	deploy := apimachineryTypes.NamespacedName{
		Namespace: "cnpg-system",
		Name:      "pgbackrest",
	}
	err = wait.PollUntilContextCancel(ctxDeploy, 5*time.Second, false,
		func(ctx context.Context) (bool, error) {
			ready, err := deployment.IsReady(ctx, cl, deploy)
			if err != nil {
				return false, fmt.Errorf("failed to check if %s is ready: %w", deploy, err)
			}
			if ready {
				return true, nil
			}

			return false, nil
		})
	if err != nil {
		Fail(fmt.Sprintf("failed to wait for deployment to be ready: %v", err))
	}

	return []byte{}
}, func(_ []byte) {})

// Run e2e tests using the Ginkgo runner.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting plugin-pgbackrest suite\n")
	RunSpecs(t, "e2e suite")
}
