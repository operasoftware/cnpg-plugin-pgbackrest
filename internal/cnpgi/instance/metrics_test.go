package instance

import (
	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/catalog"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getRecoveryWindow", func() {
	It("returns error for nil catalog", func() {
		_, _, err := getRecoveryWindow(nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("no backups found"))
	})

	It("returns error for empty catalog", func() {
		c := &catalog.Catalog{}
		_, _, err := getRecoveryWindow(c)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("no backups found"))
	})

	It("returns correct timestamps for a single completed backup", func() {
		c := &catalog.Catalog{
			Backups: []catalog.PgbackrestBackup{
				{
					Time: catalog.PgbackrestBackupTime{Start: 1000, Stop: 2000},
					WAL:  catalog.PgbackrestBackupWALArchive{Start: "000000010000000000000001", Stop: "000000010000000000000002"},
				},
			},
		}
		first, last, err := getRecoveryWindow(c)
		Expect(err).NotTo(HaveOccurred())
		Expect(first).To(Equal(int64(2000)))
		Expect(last).To(Equal(int64(2000)))
	})

	It("returns first recoverability from first backup and last from last backup", func() {
		c := &catalog.Catalog{
			Backups: []catalog.PgbackrestBackup{
				{
					Time: catalog.PgbackrestBackupTime{Start: 1000, Stop: 2000},
					WAL:  catalog.PgbackrestBackupWALArchive{Start: "000000010000000000000001", Stop: "000000010000000000000002"},
				},
				{
					Time: catalog.PgbackrestBackupTime{Start: 3000, Stop: 4000},
					WAL:  catalog.PgbackrestBackupWALArchive{Start: "000000010000000000000003", Stop: "000000010000000000000004"},
				},
				{
					Time: catalog.PgbackrestBackupTime{Start: 5000, Stop: 6000},
					WAL:  catalog.PgbackrestBackupWALArchive{Start: "000000010000000000000005", Stop: "000000010000000000000006"},
				},
			},
		}
		first, last, err := getRecoveryWindow(c)
		Expect(err).NotTo(HaveOccurred())
		Expect(first).To(Equal(int64(2000)))
		Expect(last).To(Equal(int64(6000)))
	})

	It("skips errored backups with Start=0", func() {
		c := &catalog.Catalog{
			Backups: []catalog.PgbackrestBackup{
				{
					Time: catalog.PgbackrestBackupTime{Start: 0, Stop: 500},
					WAL:  catalog.PgbackrestBackupWALArchive{Start: "000000010000000000000001", Stop: "000000010000000000000002"},
				},
				{
					Time: catalog.PgbackrestBackupTime{Start: 1000, Stop: 2000},
					WAL:  catalog.PgbackrestBackupWALArchive{Start: "000000010000000000000003", Stop: "000000010000000000000004"},
				},
			},
		}
		first, last, err := getRecoveryWindow(c)
		Expect(err).NotTo(HaveOccurred())
		Expect(first).To(Equal(int64(2000)))
		Expect(last).To(Equal(int64(2000)))
	})

	It("skips errored backups with Stop=0", func() {
		c := &catalog.Catalog{
			Backups: []catalog.PgbackrestBackup{
				{
					Time: catalog.PgbackrestBackupTime{Start: 1000, Stop: 0},
					WAL:  catalog.PgbackrestBackupWALArchive{Start: "000000010000000000000001", Stop: "000000010000000000000002"},
				},
				{
					Time: catalog.PgbackrestBackupTime{Start: 3000, Stop: 4000},
					WAL:  catalog.PgbackrestBackupWALArchive{Start: "000000010000000000000003", Stop: "000000010000000000000004"},
				},
			},
		}
		first, last, err := getRecoveryWindow(c)
		Expect(err).NotTo(HaveOccurred())
		Expect(first).To(Equal(int64(4000)))
		Expect(last).To(Equal(int64(4000)))
	})

	It("returns error when all backups are errored", func() {
		c := &catalog.Catalog{
			Backups: []catalog.PgbackrestBackup{
				{
					Time: catalog.PgbackrestBackupTime{Start: 0, Stop: 0},
					WAL:  catalog.PgbackrestBackupWALArchive{Start: "000000010000000000000001", Stop: "000000010000000000000002"},
				},
				{
					Time: catalog.PgbackrestBackupTime{Start: 1000, Stop: 0},
					WAL:  catalog.PgbackrestBackupWALArchive{Start: "000000010000000000000003", Stop: "000000010000000000000004"},
				},
			},
		}
		_, _, err := getRecoveryWindow(c)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("no successful backups found"))
	})
})
