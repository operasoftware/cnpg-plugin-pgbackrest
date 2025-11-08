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

package command

import (
	"strings"

	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pgbackrestWalRestoreOptions", func() {
	var storageConf *pgbackrestApi.PgbackrestConfiguration
	BeforeEach(func() {
		storageConf = &pgbackrestApi.PgbackrestConfiguration{
			Repositories: []pgbackrestApi.PgbackrestRepository{
				{
					Bucket:          "bucket-name",
					DestinationPath: "/",
				},
			},
		}
	})

	It("should generate correct arguments without the wal stanza", func(ctx SpecContext) {
		options, err := CloudWalRestoreOptions(ctx, storageConf, "test-cluster", "/var/lib/postgres/pgdata")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(options, " ")).
			To(
				Equal(
					"--repo1-type s3 --repo1-s3-bucket bucket-name --repo1-path / --pg1-path /var/lib/postgres/pgdata --stanza test-cluster",
				))
	})

	It("should generate correct arguments", func(ctx SpecContext) {
		extraOptions := []string{"--protocol-timeout=60"}
		storageConf.Wal = &pgbackrestApi.WalBackupConfiguration{
			RestoreAdditionalCommandArgs: extraOptions,
		}
		options, err := CloudWalRestoreOptions(ctx, storageConf, "test-cluster", "/var/lib/postgres/pgdata")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(options, " ")).
			To(
				Equal(
					"--repo1-type s3 --repo1-s3-bucket bucket-name --repo1-path / --pg1-path /var/lib/postgres/pgdata --stanza test-cluster --protocol-timeout=60",
				))
	})
})

var _ = Describe("PgbackrestRetention", func() {
	var config *pgbackrestApi.PgbackrestConfiguration
	var history int32 = 8
	retention := pgbackrestApi.PgbackrestRetention{
		Archive:     5,
		ArchiveType: "full",
		Full:        6,
		FullType:    "count",
		Diff:        7,
		History:     &history,
	}
	BeforeEach(func() {
		config = &pgbackrestApi.PgbackrestConfiguration{
			Repositories: []pgbackrestApi.PgbackrestRepository{
				{
					Retention: &retention,
				},
			},
		}
	})

	It("should generate correct argument list for retention policy", func(ctx SpecContext) {
		var empty []string
		options, err := AppendRetentionOptionsFromConfiguration(ctx, empty, config)
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(options, " ")).
			To(
				And(
					ContainSubstring("--repo1-retention-archive-type full"),
					ContainSubstring("--repo1-retention-full-type count"),
					ContainSubstring("--repo1-retention-archive 5"),
					ContainSubstring("--repo1-retention-full 6"),
					ContainSubstring("--repo1-retention-diff 7"),
					ContainSubstring("--repo1-retention-history 8"),
				))
	})
})
