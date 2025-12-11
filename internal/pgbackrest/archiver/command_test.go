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
	"path/filepath"
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
		archiver, err := New(ctx, nil, "/tmp/pgbackrest-test-spool", "pgdata", tempEmptyWalArchivePath)
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

var _ = Describe("GatherWALFilesToArchive", func() {
	var tempPgData string
	var archiveStatusDir string
	var archiver *WALArchiver

	BeforeEach(func(ctx SpecContext) {
		var err error

		tempPgData, err = os.MkdirTemp("", "pgdata-test-*")
		Expect(err).ToNot(HaveOccurred())

		// Archiver uses the env variable to determine directory root.
		os.Setenv("PGDATA", tempPgData)

		archiveStatusDir = filepath.Join(tempPgData, "pg_wal", "archive_status")
		err = os.MkdirAll(archiveStatusDir, 0755)
		Expect(err).ToNot(HaveOccurred())

		tempEmptyWalArchivePath := filepath.Join(tempPgData, "empty-wal-archive")
		_, err = os.Create(tempEmptyWalArchivePath)
		Expect(err).ToNot(HaveOccurred())

		archiver, err = New(ctx, nil, filepath.Join(tempPgData, "spool"), tempPgData, tempEmptyWalArchivePath)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// Clean up temp directory
		if tempPgData != "" {
			os.RemoveAll(tempPgData)
		}
		os.Unsetenv("PGDATA")
	})

	// Helper function to create .ready files
	createReadyFile := func(walName string) {
		path := filepath.Join(archiveStatusDir, walName+".ready")
		err := os.WriteFile(path, []byte{}, 0644)
		Expect(err).ToNot(HaveOccurred())
	}

	Context("when parallel=1", func() {
		It("should gather only the requested file", func(ctx SpecContext) {
			// Create .ready files for multiple WAL files
			createReadyFile("000000010000000000000001")
			createReadyFile("000000010000000000000002")
			createReadyFile("000000010000000000000003")

			walList := archiver.GatherWALFilesToArchive(ctx, "pg_wal/000000010000000000000001", 1)

			Expect(walList).To(ConsistOf("pg_wal/000000010000000000000001"))
		})

		It("should handle when no other .ready files exist", func(ctx SpecContext) {
			// Only create the requested file
			createReadyFile("000000010000000000000001")

			walList := archiver.GatherWALFilesToArchive(ctx, "pg_wal/000000010000000000000001", 1)

			Expect(walList).To(ConsistOf("pg_wal/000000010000000000000001"))
		})
	})

	Context("when parallel>1", func() {
		It("should gather multiple files when parallel=4", func(ctx SpecContext) {
			// Create .ready files for multiple WAL files
			createReadyFile("000000010000000000000001")
			createReadyFile("000000010000000000000002")
			createReadyFile("000000010000000000000003")
			createReadyFile("000000010000000000000004")

			walList := archiver.GatherWALFilesToArchive(ctx, "pg_wal/000000010000000000000001", 4)

			Expect(walList).To(ConsistOf(
				"pg_wal/000000010000000000000001",
				"pg_wal/000000010000000000000002",
				"pg_wal/000000010000000000000003",
				"pg_wal/000000010000000000000004",
			))
		})

		It("should not exceed parallel limit even when more files are ready", func(ctx SpecContext) {
			// Create many .ready files
			for i := 1; i <= 10; i++ {
				createReadyFile("00000001000000000000000" + string(rune('0'+i)))
			}

			walList := archiver.GatherWALFilesToArchive(ctx, "pg_wal/000000010000000000000001", 3)

			Expect(walList).To(HaveLen(3))
		})

		It("should handle when fewer files exist than parallel limit", func(ctx SpecContext) {
			// Create only 2 .ready files but request parallel=5
			createReadyFile("000000010000000000000001")
			createReadyFile("000000010000000000000002")

			walList := archiver.GatherWALFilesToArchive(ctx, "pg_wal/000000010000000000000001", 5)

			// Should only get the files that exist
			Expect(walList).To(ConsistOf(
				"pg_wal/000000010000000000000001",
				"pg_wal/000000010000000000000002",
			))
		})
	})

	Context("edge cases", func() {
		It("should handle empty archive_status directory", func(ctx SpecContext) {
			// Don't create any .ready files

			walList := archiver.GatherWALFilesToArchive(ctx, "pg_wal/000000010000000000000001", 3)

			// Should still return the requested file
			Expect(walList).To(ConsistOf("pg_wal/000000010000000000000001"))
		})

	})

	Context("other files in directory", func() {
		It("should ignore non-.ready files in archive_status", func(ctx SpecContext) {
			// Create .ready files
			createReadyFile("000000010000000000000001")
			createReadyFile("000000010000000000000002")

			// Create .done files (should be ignored)
			donePath := filepath.Join(archiveStatusDir, "000000010000000000000003.done")
			err := os.WriteFile(donePath, []byte{}, 0644)
			Expect(err).ToNot(HaveOccurred())

			// Create a random file (should be ignored)
			randomPath := filepath.Join(archiveStatusDir, "random.txt")
			err = os.WriteFile(randomPath, []byte{}, 0644)
			Expect(err).ToNot(HaveOccurred())

			walList := archiver.GatherWALFilesToArchive(ctx, "pg_wal/000000010000000000000001", 5)

			// Should only get .ready files, not .done or other files
			Expect(walList).To(ConsistOf(
				"pg_wal/000000010000000000000001",
				"pg_wal/000000010000000000000002",
			))
		})
		It("should handle timeline history files", func(ctx SpecContext) {
			// Create timeline history file
			createReadyFile("00000002.history")
			createReadyFile("000000010000000000000001")

			walList := archiver.GatherWALFilesToArchive(ctx, "pg_wal/00000002.history", 2)

			Expect(walList).To(ConsistOf(
				"pg_wal/00000002.history",
				"pg_wal/000000010000000000000001",
			))
		})
	})
})
