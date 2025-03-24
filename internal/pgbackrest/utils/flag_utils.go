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
)

func FormatRepoFlag(repository int, flag string) string {
	// Repositories must be indexed from 1 while all iterations are 0-based.
	// Using repo0 causes a segfault.
	return fmt.Sprintf("--repo%d-%s", repository+1, flag)
}

func FormatDbFlag(database int, flag string) string {
	// Databases must be indexed from 1 while all iterations are 0-based.
	return fmt.Sprintf("--pg%d-%s", database+1, flag)
}
