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

package archiver

import (
	"os"
	"strings"

	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pgbackrestWalArchiveOptions", func() {
	var config *pgbackrestApi.PgbackrestConfiguration
	var tempDir string
	var tempEmptyWalArchivePath string

	BeforeEach(func() {
		config = &pgbackrestApi.PgbackrestConfiguration{
			Compression: "gzip",

			Repositories: []pgbackrestApi.PgbackrestRepository{
				{
					// TODO: Add tests for env generation to ensure encryption and bucket access variables are inserted properly.
					// Encryption:      "aes-256-cbc",
					Bucket:          "bucket-name",
					DestinationPath: "/",
				},
			},
			Wal: &pgbackrestApi.WalBackupConfiguration{
				// TODO: Add some custom args?
			},
		}
		var err error
		tempDir, err = os.MkdirTemp(os.TempDir(), "command_test")
		Expect(err).ToNot(HaveOccurred())
		file, err := os.CreateTemp(tempDir, "empty-wal-archive-path")
		Expect(err).ToNot(HaveOccurred())
		tempEmptyWalArchivePath = file.Name()
	})
	AfterEach(func() {
		err := os.RemoveAll(tempDir)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should generate correct arguments", func(ctx SpecContext) {
		archiver, err := New(ctx, nil, "spool", "pgdata", tempEmptyWalArchivePath)
		Expect(err).ToNot(HaveOccurred())

		extraOptions := []string{"--buffer-size=5MB", "--io-timeout=60"}
		config.Wal.ArchiveAdditionalCommandArgs = extraOptions
		options, err := archiver.PgbackrestWalArchiveOptions(ctx, config, "test-cluster")
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(options, " ")).
			To(
				Equal(
					"--compress-type gzip --buffer-size=5MB --io-timeout=60 --repo1-type s3 --repo1-s3-bucket bucket-name --repo1-path / --stanza test-cluster",
				))
	})
})
