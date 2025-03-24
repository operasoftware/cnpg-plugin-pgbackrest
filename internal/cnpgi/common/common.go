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

package common

import (
	"fmt"
	"strings"

	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
)

// TODO: refactor.
const (
	// ScratchDataDirectory is the directory to be used for scratch data.
	ScratchDataDirectory = "/controller"

	// CertificatesDir location to store the certificates.
	CertificatesDir = ScratchDataDirectory + "/certificates/"

	// BarmanBackupEndpointCACertificateLocation is the location where the barman endpoint
	// CA certificate is stored.
	BarmanBackupEndpointCACertificateLocation = CertificatesDir + BarmanBackupEndpointCACertificateFileName

	// BarmanBackupEndpointCACertificateFileName is the name of the file in which the barman endpoint
	// CA certificate for backups is stored.
	BarmanBackupEndpointCACertificateFileName = "backup-" + BarmanEndpointCACertificateFileName

	// BarmanRestoreEndpointCACertificateFileName is the name of the file in which the barman endpoint
	// CA certificate for restores is stored.
	BarmanRestoreEndpointCACertificateFileName = "restore-" + BarmanEndpointCACertificateFileName

	// BarmanEndpointCACertificateFileName is the name of the file in which the barman endpoint
	// CA certificate is stored.
	BarmanEndpointCACertificateFileName = "barman-ca.crt"
)

// GetRestoreCABundleEnv gets the environment variables to be used when custom
// Object Store CA is present
func GetRestoreCABundleEnv(configuration *pgbackrestApi.PgbackrestConfiguration) []string {
	var env []string

	if configuration.Repositories[0].EndpointCA != nil && configuration.Repositories[0].AWS != nil {
		env = append(env, fmt.Sprintf("AWS_CA_BUNDLE=%s", BarmanBackupEndpointCACertificateLocation))
	}
	return env
}

// MergeEnv merges all the values inside incomingEnv into env.
func MergeEnv(env []string, incomingEnv []string) []string {
	result := make([]string, len(env), len(env)+len(incomingEnv))
	copy(result, env)

	for _, incomingItem := range incomingEnv {
		incomingKV := strings.SplitAfterN(incomingItem, "=", 2)
		if len(incomingKV) != 2 {
			continue
		}

		found := false
		for idx, item := range result {
			if strings.HasPrefix(item, incomingKV[0]) {
				result[idx] = incomingItem
				found = true
			}
		}
		if !found {
			result = append(result, incomingItem)
		}
	}

	return result
}
