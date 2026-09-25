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

package common

import (
	"fmt"
	"sync"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"
)

// DestinationCheck remembers the archive destination that the last WAL batch was
// successfully pushed to. While batches keep succeeding against the same destination,
// Archive skips CheckWalArchiveDestination, which runs "pgbackrest info" against the
// repository. A failed batch or a change of the destination brings the check back.
//
// A nil *DestinationCheck remembers nothing, so the check runs for every batch.
type DestinationCheck struct {
	mu       sync.Mutex
	verified string
}

func (d *DestinationCheck) isVerified(key string) bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.verified == key
}

func (d *DestinationCheck) markVerified(key string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.verified = key
}

func (d *DestinationCheck) reset() {
	d.markVerified("")
}

// destinationKey identifies where a WAL batch is pushed to. Any change to the Archive
// spec bumps its generation, and the stanza comes from the Cluster plugin parameters.
func destinationKey(archive *pgbackrestv1.Archive, stanza string) string {
	return fmt.Sprintf("%s/%d/%s", archive.UID, archive.Generation, stanza)
}
