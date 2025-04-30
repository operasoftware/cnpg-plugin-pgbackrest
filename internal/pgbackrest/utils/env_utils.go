/*
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

package utils

import (
	"fmt"
	"os"
	"regexp"
)

// PgbackrestServiceEnvVarPattern should match all service discovery environment variables injected to
// the pod by Kubernetes for services with names starting with "pgbackrest".
var PgbackrestServiceEnvVarPattern = regexp.MustCompile("^PGBACKREST_(?:[A-Z0-9]+_)*(?:PORT|SERVICE)")

// FormatEnv takes an environment variable name and its value. It returns a properly
// formatted variable with a prefix used by pgBackRest to detect its config variables.
// Returned value is ready to be passed as a part of the command's env array.
func FormatEnv(env string, value string) string {
	return fmt.Sprintf("PGBACKREST_%s=%s", env, value)
}

// FormatRepoEnv takes a zero-based repository index, an environment variable name and
// its value. It returns a properly formatted repo-scoped variable with a prefix used
// by pgBackRest to detect its config variables.
// Returned value is ready to be passed as a part of the command's env array.
func FormatRepoEnv(repository int, env string, value string) string {
	return fmt.Sprintf("PGBACKREST_REPO%d_%s=%s", repository+1, env, value)
}

// FormatDbEnv takes a zero-based database index, an environment variable name and
// its value. It returns a properly formatted db-scoped variable with a prefix used
// by pgBackRest to detect its config variables.
// Returned value is ready to be passed as a part of the command's env array.
func FormatDbEnv(database int, env string, value string) string {
	return fmt.Sprintf("PGBACKREST_PG%d_%s=%s", database, env, value)
}

func stripServicesFromEnv(env []string) (filteredEnv []string) {
	for _, variable := range env {
		if !PgbackrestServiceEnvVarPattern.MatchString(variable) {
			filteredEnv = append(filteredEnv, variable)
		}
	}
	return filteredEnv
}

// SanitizedEnviron returns a copy of the environment variables list with variables
// added for Kubernetes services with names starting from "pgbackrest" removed.
// Those variables cause pgbackrest to output warnings during configuration parsing
// and those messages always go to the standard output causing issues with
// "pgbackrest info" calls as those should only output JSON.
// In addition, removing them makes log entries much clearer.
func SanitizedEnviron() []string {
	return stripServicesFromEnv(os.Environ())
}
