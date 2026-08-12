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

package credentials

import (
	machineryapi "github.com/cloudnative-pg/machinery/pkg/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("envSetAzureCredentials", func() {
	const namespace = "default"

	var (
		cl          client.Client
		credentials *pgbackrestApi.AzureCredentials
	)

	buildClient := func(objects ...client.Object) client.Client {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	}

	BeforeEach(func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "azure-secret",
			},
			Data: map[string][]byte{
				"AZURE_STORAGE_ACCOUNT": []byte("storageaccountname"),
				"AZURE_STORAGE_KEY":     []byte("c3RvcmFnZWFjY291bnRrZXk="),
			},
		}
		cl = buildClient(secret)
		credentials = &pgbackrestApi.AzureCredentials{
			KeyType: pgbackrestApi.AzureKeyTypeShared,
			Account: &machineryapi.SecretKeySelector{
				LocalObjectReference: machineryapi.LocalObjectReference{Name: "azure-secret"},
				Key:                  "AZURE_STORAGE_ACCOUNT",
			},
			Key: &machineryapi.SecretKeySelector{
				LocalObjectReference: machineryapi.LocalObjectReference{Name: "azure-secret"},
				Key:                  "AZURE_STORAGE_KEY",
			},
		}
	})

	It("exports the Azure environment variables from the referenced secret", func(ctx SpecContext) {
		env, err := envSetAzureCredentials(ctx, cl, namespace, credentials, "", 0, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(env).To(ConsistOf(
			"PGBACKREST_REPO1_AZURE_ACCOUNT=storageaccountname",
			"PGBACKREST_REPO1_AZURE_KEY=c3RvcmFnZWFjY291bnRrZXk=",
			"PGBACKREST_REPO1_AZURE_KEY_TYPE=shared",
		))
	})

	It("exports the endpoint override as an environment variable when configured", func(ctx SpecContext) {
		env, err := envSetAzureCredentials(ctx, cl, namespace, credentials, "azurite:10000", 0, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(env).To(ContainElement("PGBACKREST_REPO1_AZURE_ENDPOINT=azurite:10000"))
	})

	It("does not export an endpoint variable when no override is configured", func(ctx SpecContext) {
		env, err := envSetAzureCredentials(ctx, cl, namespace, credentials, "", 0, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(env).ToNot(ContainElement(ContainSubstring("AZURE_ENDPOINT")))
	})

	It("fails when the storage account reference is missing", func(ctx SpecContext) {
		credentials.Account = nil
		_, err := envSetAzureCredentials(ctx, cl, namespace, credentials, "", 0, nil)
		Expect(err).To(MatchError(ContainSubstring("missing Azure storage account")))
	})

	It("fails when the account key reference is missing", func(ctx SpecContext) {
		credentials.Key = nil
		_, err := envSetAzureCredentials(ctx, cl, namespace, credentials, "", 0, nil)
		Expect(err).To(MatchError(ContainSubstring("missing Azure account key")))
	})

	It("fails when the referenced secret key does not exist", func(ctx SpecContext) {
		credentials.Key.Key = "MISSING_KEY"
		_, err := envSetAzureCredentials(ctx, cl, namespace, credentials, "", 0, nil)
		Expect(err).To(MatchError(ContainSubstring("missing key MISSING_KEY")))
	})
})

var _ = Describe("envSetCloudCredentials", func() {
	const namespace = "default"

	buildClient := func(objects ...client.Object) client.Client {
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	}

	It("fails fast when a repository sets both s3 and azure credentials", func(ctx SpecContext) {
		cl := buildClient()
		configuration := &pgbackrestApi.PgbackrestConfiguration{
			Repositories: []pgbackrestApi.PgbackrestRepository{
				{
					PgbackrestCredentials: pgbackrestApi.PgbackrestCredentials{
						AWS:   &pgbackrestApi.S3Credentials{},
						Azure: &pgbackrestApi.AzureCredentials{},
					},
				},
			},
		}
		_, err := envSetCloudCredentials(ctx, cl, namespace, configuration, nil)
		Expect(err).To(MatchError(ContainSubstring("both s3Credentials and azureCredentials set")))
	})
})
