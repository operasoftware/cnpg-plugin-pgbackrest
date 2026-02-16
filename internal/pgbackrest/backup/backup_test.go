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

package backup

import (
	"fmt"
	"strings"

	cnpgApiV1 "github.com/cloudnative-pg/api/pkg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
	pgbackrestCatalog "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/catalog"
)

var _ = Describe("GetPgbackrestBackupOptions", func() {
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
		backupConfig := cnpgApiV1.BackupPluginConfiguration{Name: metadata.PluginName}
		command := NewBackupCommand(pluginConfig, &backupConfig, pgDataDir)

		options, err := command.GetPgbackrestBackupOptions(ctx, backupName, stanza)

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(options, " ")).
			To(
				Equal(
					fmt.Sprintf("backup --annotation %s=%s --repo1-type s3 --repo1-s3-bucket bucket-name --repo1-path / --pg1-path %s --pg1-user postgres --pg1-socket-path /controller/run/ --log-level-stderr warn --log-level-console off --log-level-file off --stanza %s --lock-path /controller/tmp/pgbackrest --no-archive-check", pgbackrestCatalog.BackupNameAnnotation, backupName, pgDataDir, stanza),
				))
	})

	It("should include options from the backup configuration", func(ctx SpecContext) {
		backupConfig := cnpgApiV1.BackupPluginConfiguration{Name: metadata.PluginName, Parameters: map[string]string{"type": "full"}}
		command := NewBackupCommand(pluginConfig, &backupConfig, pgDataDir)

		options, err := command.GetPgbackrestBackupOptions(ctx, backupName, stanza)

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(options, " ")).
			To(ContainSubstring(" --type=full "))
	})

	It("should include custom annotations", func(ctx SpecContext) {
		backupConfig := cnpgApiV1.BackupPluginConfiguration{Name: metadata.PluginName, Parameters: map[string]string{"type": "full"}}
		pluginConfig.Data = &pgbackrestApi.DataBackupConfiguration{
			Annotations: map[string]string{"foo": "bar"},
		}
		command := NewBackupCommand(pluginConfig, &backupConfig, pgDataDir)

		options, err := command.GetPgbackrestBackupOptions(ctx, backupName, stanza)

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(options, " ")).
			To(ContainSubstring(" --annotation foo=bar "))
	})

	It("should include Full backup retention", func(ctx SpecContext) {
		backupConfig := cnpgApiV1.BackupPluginConfiguration{Name: metadata.PluginName, Parameters: map[string]string{"type": "full"}}
		retention := pgbackrestApi.PgbackrestRetention{
			Full:     28,
			FullType: "time",
		}
		pluginConfig.Repositories[0].Retention = &retention
		command := NewBackupCommand(pluginConfig, &backupConfig, pgDataDir)

		options, err := command.GetPgbackrestBackupOptions(ctx, backupName, stanza)

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(options, " ")).
			To(
				And(
					ContainSubstring(" --repo1-retention-full 28 "),
					ContainSubstring(" --repo1-retention-full-type time "),
				),
			)
	})

	It("should include Archive backup retention", func(ctx SpecContext) {
		backupConfig := cnpgApiV1.BackupPluginConfiguration{Name: metadata.PluginName, Parameters: map[string]string{"type": "full"}}
		retention := pgbackrestApi.PgbackrestRetention{
			Archive:     10,
			ArchiveType: "time",
		}
		pluginConfig.Repositories[0].Retention = &retention
		command := NewBackupCommand(pluginConfig, &backupConfig, pgDataDir)

		options, err := command.GetPgbackrestBackupOptions(ctx, backupName, stanza)

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(options, " ")).
			To(
				And(
					ContainSubstring(" --repo1-retention-archive 10 "),
					ContainSubstring(" --repo1-retention-archive-type time "),
				),
			)
	})

	It("should include History retention", func(ctx SpecContext) {
		backupConfig := cnpgApiV1.BackupPluginConfiguration{Name: metadata.PluginName, Parameters: map[string]string{"type": "full"}}
		var hist int32 = 9
		retention := pgbackrestApi.PgbackrestRetention{
			History: &hist,
		}
		pluginConfig.Repositories[0].Retention = &retention
		command := NewBackupCommand(pluginConfig, &backupConfig, pgDataDir)

		options, err := command.GetPgbackrestBackupOptions(ctx, backupName, stanza)

		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(options, " ")).
			To(
				ContainSubstring(" --repo1-retention-history 9"),
			)
	})
})
