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

package restorer

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
)

var _ = Describe("GetPgbackrestRestoreOptions", func() {
	var pluginConfig *pgbackrestApi.PgbackrestConfiguration
	pgDataDir := "/pg/data"
	backupName := "test-backup"
	stanza := "cluster-name"

	BeforeEach(func() {
		pluginConfig = &pgbackrestApi.PgbackrestConfiguration{
			Repositories: []pgbackrestApi.PgbackrestRepository{
				{
					Bucket:          "bucket-name",
					DestinationPath: "/",
				},
			},
		}
	})

	It("should generate correct arguments", func(ctx SpecContext) {
		command := NewRestoreCommand(pluginConfig, pgDataDir)

		options, err := command.GetPgbackrestRestoreOptions(ctx, backupName, stanza)

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(options, " ")).
			To(
				And(
					ContainSubstring("--lock-path /controller/tmp/pgbackrest"),
					ContainSubstring("--repo1-type s3"),
					ContainSubstring("--repo1-s3-bucket bucket-name"),
					ContainSubstring("--repo1-path /"),
					ContainSubstring("--pg1-path %s", pgDataDir),
					ContainSubstring("--log-level-stderr warn"),
					ContainSubstring("--log-level-console off"),
					ContainSubstring("--stanza %s", stanza),
					ContainSubstring("restore --set %s", backupName),
				),
			)
	})

	It("should include options from the restore configuration", func(ctx SpecContext) {
		pluginConfig.Restore = &pgbackrestApi.DataRestoreConfiguration{
			AdditionalCommandArgs: []string{"--db-exclude=foo", "--db-include=bar"},
		}
		command := NewRestoreCommand(pluginConfig, pgDataDir)

		options, err := command.GetPgbackrestRestoreOptions(ctx, backupName, stanza)

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(options, " ")).
			To(
				And(
					ContainSubstring("--db-exclude=foo"),
					ContainSubstring("--db-include=bar"),
				),
			)
	})

	It("should include job parallelism", func(ctx SpecContext) {
		jobs := int32(4)
		pluginConfig.Restore = &pgbackrestApi.DataRestoreConfiguration{
			Jobs: &jobs,
		}
		command := NewRestoreCommand(pluginConfig, pgDataDir)

		options, err := command.GetPgbackrestRestoreOptions(ctx, backupName, stanza)

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(options, " ")).
			To(ContainSubstring("--process-max 4"))
	})
})
