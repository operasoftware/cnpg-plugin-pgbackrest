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

// Package credentials handles the retrieval and injection of credentials stored in
// Kubernetes secrets
package credentials

import (
	"context"
	"fmt"

	machineryapi "github.com/cloudnative-pg/machinery/pkg/api"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/utils"
)

const (
	// ScratchDataDirectory is the directory to be used for scratch data
	ScratchDataDirectory = "/controller"

	// CertificatesDir location to store the certificates
	CertificatesDir = ScratchDataDirectory + "/certificates/"

	// TODO: Properly mount/read and pass CA file for each pgbackrest repository.

	// BarmanBackupEndpointCACertificateLocation is the location where the barman endpoint
	// CA certificate is stored
	BarmanBackupEndpointCACertificateLocation = CertificatesDir + BarmanBackupEndpointCACertificateFileName

	// BarmanBackupEndpointCACertificateFileName is the name of the file in which the barman endpoint
	// CA certificate for backups is stored
	BarmanBackupEndpointCACertificateFileName = "backup-" + BarmanEndpointCACertificateFileName

	// BarmanRestoreEndpointCACertificateLocation is the location where the barman endpoint
	// CA certificate is stored
	BarmanRestoreEndpointCACertificateLocation = CertificatesDir + BarmanRestoreEndpointCACertificateFileName

	// BarmanRestoreEndpointCACertificateFileName is the name of the file in which the barman endpoint
	// CA certificate for restores is stored
	BarmanRestoreEndpointCACertificateFileName = "restore-" + BarmanEndpointCACertificateFileName

	// BarmanEndpointCACertificateFileName is the name of the file in which the barman endpoint
	// CA certificate is stored
	BarmanEndpointCACertificateFileName = "barman-ca.crt"
)

// EnvSetBackupCloudCredentials sets the AWS environment variables needed for backups
// given the configuration inside the cluster
func EnvSetBackupCloudCredentials(
	ctx context.Context,
	c client.Client,
	namespace string,
	configuration *pgbackrestApi.PgbackrestConfiguration,
	env []string,
) ([]string, error) {
	for index, repo := range configuration.Repositories {
		if repo.EndpointCA != nil {
			env = append(env, utils.FormatRepoEnv(index, "HOST_CA_FILE", BarmanBackupEndpointCACertificateLocation))
		}
	}

	return envSetCloudCredentials(ctx, c, namespace, configuration, env)
}

// EnvSetRestoreCloudCredentials sets the AWS environment variables needed for restores
// given the configuration inside the cluster
func EnvSetRestoreCloudCredentials(
	ctx context.Context,
	c client.Client,
	namespace string,
	configuration *pgbackrestApi.PgbackrestConfiguration,
	env []string,
) ([]string, error) {
	for index, repo := range configuration.Repositories {
		if repo.EndpointCA != nil {
			env = append(env, utils.FormatRepoEnv(index, "HOST_CA_FILE", BarmanBackupEndpointCACertificateLocation))
		}
	}

	return envSetCloudCredentials(ctx, c, namespace, configuration, env)
}

// envSetCloudCredentials sets the AWS environment variables given the configuration
// inside the cluster
func envSetCloudCredentials(
	ctx context.Context,
	c client.Client,
	namespace string,
	configuration *pgbackrestApi.PgbackrestConfiguration,
	env []string,
) (envs []string, err error) {
	for index, repo := range configuration.Repositories {
		if repo.AWS != nil {
			env, err = envSetAWSCredentials(ctx, c, namespace, repo.AWS, index, env)
			if err != nil {
				return nil, err
			}
		}
		if len(repo.Encryption) != 0 {
			env, err = envSetEncryptionCredentials(ctx, c, repo.Encryption, repo.EncryptionKey, namespace, index, env)
			if err != nil {
				return nil, err
			}
		}
	}
	return env, nil
}

// envSetAWSCredentials sets the AWS environment variables given the configuration
// inside the cluster
func envSetAWSCredentials(
	ctx context.Context,
	client client.Client,
	namespace string,
	s3credentials *pgbackrestApi.S3Credentials,
	repoIndex int,
	env []string,
) ([]string, error) {
	// check if AWS credentials are defined
	if s3credentials == nil {
		return nil, fmt.Errorf("missing S3 credentials")
	}

	// Get access key ID
	if s3credentials.AccessKeyIDReference == nil {
		return nil, fmt.Errorf("missing access key ID")
	}
	accessKeyID, accessKeyErr := extractValueFromSecret(
		ctx,
		client,
		s3credentials.AccessKeyIDReference,
		namespace,
	)
	if accessKeyErr != nil {
		return nil, accessKeyErr
	}

	// Get secret access key
	if s3credentials.SecretAccessKeyReference == nil {
		return nil, fmt.Errorf("missing secret access key")
	}
	secretAccessKey, secretAccessErr := extractValueFromSecret(
		ctx,
		client,
		s3credentials.SecretAccessKeyReference,
		namespace,
	)
	if secretAccessErr != nil {
		return nil, secretAccessErr
	}

	env = append(env, utils.FormatRepoEnv(repoIndex, "S3_REGION", s3credentials.Region))
	env = append(env, utils.FormatRepoEnv(repoIndex, "S3_KEY", string(accessKeyID)))
	env = append(env, utils.FormatRepoEnv(repoIndex, "S3_KEY_SECRET", string(secretAccessKey)))

	return env, nil
}

// envSetEncryptionCredentials sets the pgbackrest encryption environment variables given
// the configuration inside the cluster
func envSetEncryptionCredentials(
	ctx context.Context,
	client client.Client,
	encryptionType pgbackrestApi.EncryptionType,
	encryptionKeyRef *machineryapi.SecretKeySelector,
	namespace string,
	repoIndex int,
	env []string,
) ([]string, error) {
	// check if encryption key is defined
	if encryptionKeyRef == nil {
		return nil, fmt.Errorf("missing encryption key")
	}

	encryptionKey, err := extractValueFromSecret(
		ctx,
		client,
		encryptionKeyRef,
		namespace,
	)
	if err != nil {
		return nil, err
	}

	env = append(env, utils.FormatRepoEnv(repoIndex, "CIPHER_TYPE", string(encryptionType)))
	env = append(env, utils.FormatRepoEnv(repoIndex, "CIPHER_PASS", string(encryptionKey)))

	return env, nil
}

func extractValueFromSecret(
	ctx context.Context,
	c client.Client,
	secretReference *machineryapi.SecretKeySelector,
	namespace string,
) ([]byte, error) {
	secret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretReference.Name}, secret)
	if err != nil {
		return nil, fmt.Errorf("while getting secret %s: %w", secretReference.Name, err)
	}

	value, ok := secret.Data[secretReference.Key]
	if !ok {
		return nil, fmt.Errorf("missing key %s, inside secret %s", secretReference.Key, secretReference.Name)
	}

	return value, nil
}
