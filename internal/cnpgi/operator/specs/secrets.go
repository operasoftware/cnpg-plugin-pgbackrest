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

package specs

import (
	machineryapi "github.com/cloudnative-pg/machinery/pkg/api"
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
)

// CollectSecretNamesFromCredentials collects the names of the secrets
func CollectSecretNamesFromCredentials(pgbackrestCredentials *pgbackrestApi.PgbackrestCredentials) []string {
	var references []*machineryapi.SecretKeySelector
	if pgbackrestCredentials.AWS != nil {
		references = append(
			references,
			pgbackrestCredentials.AWS.AccessKeyIDReference,
			pgbackrestCredentials.AWS.SecretAccessKeyReference,
		)
	}

	result := make([]string, 0, len(references))
	for _, reference := range references {
		if reference == nil {
			continue
		}
		result = append(result, reference.Name)
	}

	return result
}
