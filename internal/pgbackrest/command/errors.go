/*
Copyright The CloudNativePG Contributors

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

package command

import (
	"context"
	"fmt"
)

const (
	// TODO: Map pgbackrest error codes.
	// Probably all codes are documented here:
	// https://github.com/pgbackrest/pgbackrest/blob/c2f64bb03bdbb2ce883041118dbcfd79da3f1307/src/build/error/error.yaml

	// Connectivity to csp was ok but operation still failed error code
	// https://docs.pgbarman.org/release/3.10.0/barman-cloud-restore.1.html
	operationErrorCode = 1

	// Network related error
	// https://docs.pgbarman.org/release/3.10.0/barman-cloud-restore.1.html
	networkErrorCode = 2

	// CLI related error
	// https://docs.pgbarman.org/release/3.10.0/barman-cloud-restore.1.html
	cliErrorCode = 3

	// General barman cloud errors
	// https://docs.pgbarman.org/release/3.10.0/barman-cloud-restore.1.html
	generalErrorCode = 4
)

// errorDescriptions are the human descriptions of the error codes
var errorDescriptions = map[int]string{
	operationErrorCode: "Operation error",
	networkErrorCode:   "Network error",
	cliErrorCode:       "CLI argument parsing error",
	generalErrorCode:   "General error",
}

// CloudRestoreError is raised when pgbackrest restore fails
type CloudRestoreError struct {
	// The exit code returned by pgbackrest
	ExitCode int

	// This is true when pgbackrest can return significant error codes
	HasRestoreErrorCodes bool
}

// Error implements the error interface
func (err *CloudRestoreError) Error() string {
	msg, ok := errorDescriptions[err.ExitCode]
	if !ok {
		msg = "Generic failure"
	}

	return fmt.Sprintf("%s (exit code %v)", msg, err.ExitCode)
}

// IsRetriable returns true whether the error is temporary, and
// it could be a good idea to retry the restore later
func (err *CloudRestoreError) IsRetriable() bool {
	return (err.ExitCode == networkErrorCode || err.ExitCode == generalErrorCode) && err.HasRestoreErrorCodes
}

// UnmarshalPgbackrestRestoreExitCode returns the correct error
// for a certain pgbackrest exit code
func UnmarshalPgbackrestRestoreExitCode(_ context.Context, exitCode int) error {
	if exitCode == 0 {
		return nil
	}

	return &CloudRestoreError{
		ExitCode:             exitCode,
		HasRestoreErrorCodes: true,
	}
}
