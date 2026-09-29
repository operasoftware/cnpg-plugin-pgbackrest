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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
)

var _ = Describe("walsToPrefetch", func() {
	const walName = "000000010000000000000010"

	DescribeTable("prefetches maxParallel - 1 + extraPrefetch WAL files",
		func(configuration *pgbackrestApi.WalBackupConfiguration, wals, parallel int) {
			prefetch, err := walsToPrefetch(walName, configuration, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(prefetch.WALs).To(HaveLen(wals))
			Expect(prefetch.Parallel).To(Equal(parallel))
			if wals > 0 {
				Expect(prefetch.WALs[0]).To(Equal("000000010000000000000011"))
			}
		},
		Entry("without a WAL configuration", nil, 0, 1),
		Entry("with maxParallel 1", &pgbackrestApi.WalBackupConfiguration{MaxParallel: 1}, 0, 1),
		Entry("with maxParallel 1, ignoring extraPrefetch",
			&pgbackrestApi.WalBackupConfiguration{MaxParallel: 1, ExtraPrefetch: 8}, 0, 1),
		Entry("with maxParallel 4", &pgbackrestApi.WalBackupConfiguration{MaxParallel: 4}, 3, 4),
		Entry("with maxParallel 4 and extraPrefetch 12",
			&pgbackrestApi.WalBackupConfiguration{MaxParallel: 4, ExtraPrefetch: 12}, 15, 4),
	)

	It("prefetches nothing for a file that is not a regular WAL", func() {
		prefetch, err := walsToPrefetch("00000002.history",
			&pgbackrestApi.WalBackupConfiguration{MaxParallel: 4, ExtraPrefetch: 12}, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(prefetch.WALs).To(BeEmpty())
	})
})
