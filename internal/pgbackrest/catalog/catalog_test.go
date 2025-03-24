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

package catalog

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// TODO: finish this list

// TODO: Finish those tests
var _ = Describe("pgbackrest info parsing", func() {
	// Pgbackrest returns info as a list of JSON objects, likely one for each repo.
	const pgbackrestInfoOutput = `[
  {
    "archive": [
      {
        "database": { "id": 1, "repo-key": 1 },
        "id": "17-1",
        "max": "00000001000000000000001F",
        "min": "000000010000000000000001"
      }
    ],
    "backup": [
      {
        "annotation": {
          "cnpg-backup-name": "backup-20250331142029",
          "dummy-annotation": "foo"
        },
        "archive": {
          "start": "000000010000000000000006",
          "stop": "000000010000000000000006"
        },
        "backrest": { "format": 5, "version": "2.54.2" },
        "database": { "id": 1, "repo-key": 1 },
        "error": false,
        "info": {
          "delta": 30690569,
          "repository": { "delta": 3661879, "size": 3661879 },
          "size": 30690569
        },
        "label": "20250331-142029F",
        "lsn": { "start": "0/6000028", "stop": "0/6000158" },
        "prior": null,
        "reference": null,
        "timestamp": { "start": 1743430829, "stop": 1743430841 },
        "type": "full"
      },
      {
        "annotation": {
          "cnpg-backup-name": "backup-20250331150451"
        },
        "archive": {
          "start": "000000010000000000000009",
          "stop": "000000010000000000000009"
        },
        "backrest": { "format": 5, "version": "2.54.2" },
        "database": { "id": 1, "repo-key": 1 },
        "error": false,
        "info": {
          "delta": 24834,
          "repository": { "delta": 1324, "size": 3661992 },
          "size": 30690569
        },
        "label": "20250331-142029F_20250331-150451I",
        "lsn": { "start": "0/9000028", "stop": "0/9000158" },
        "prior": "20250331-142029F",
        "reference": ["20250331-142029F"],
        "timestamp": { "start": 1743433491, "stop": 1743433492 },
        "type": "incr"
      },
      {
        "annotation": {
          "cnpg-backup-name": "backup-20250401132030"
        },
        "archive": {
          "start": "00000001000000000000000D",
          "stop": "00000001000000000000000D"
        },
        "backrest": { "format": 5, "version": "2.54.2" },
        "database": { "id": 1, "repo-key": 1 },
        "error": false,
        "info": {
          "delta": 24833,
          "repository": { "delta": 1440, "size": 3662108 },
          "size": 30690568
        },
        "label": "20250401-132030I",
        "lsn": { "start": "0/D000028", "stop": "0/D000158" },
        "prior": null,
        "reference": null,
        "timestamp": { "start": 1743513630, "stop": 1743513632 },
        "type": "full"
      }
    ],
    "cipher": "none",
    "db": [
      { "id": 1, "repo-key": 1, "system-id": 7487970936345972767, "version": "17" }
    ],
    "name": "cluster-example-pgbackrest",
    "repo": [{ "cipher": "none", "key": 1, "status": { "code": 0, "message": "ok" } }],
    "status": { "code": 0, "lock": { "backup": { "held": false } }, "message": "ok" }
  }
]`

	It("must parse a correct output", func() {
		result, err := NewCatalogFromPgbackrestInfo(pgbackrestInfoOutput)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Backups).To(HaveLen(3))
		Expect(result.Backups[0].ID).To(Equal("20250331-142029F"))
		Expect(result.Backups[0].Time.Start).To(Equal(int64(1743430829)))
		Expect(result.Backups[0].Time.Stop).To(Equal(int64(1743430841)))
		Expect(result.Databases[0].SystemID).To(Equal(int64(7487970936345972767)))
	})

	It("must extract the latest backup id", func() {
		result, err := NewCatalogFromPgbackrestInfo(pgbackrestInfoOutput)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.LatestBackupInfo().ID).To(Equal("20250401-132030I"))
	})

	It("can detect the first recoverability point", func() {
		result, err := NewCatalogFromPgbackrestInfo(pgbackrestInfoOutput)
		Expect(err).ToNot(HaveOccurred())
		Expect((*result.FirstRecoverabilityPoint()).In(time.UTC)).To(
			Equal(time.Date(2025, 3, 31, 14, 20, 41, 0, time.UTC)))
	})

	// It("can find the closest backup info when there is one", func() {
	// 	recoveryTarget := &v1.RecoveryTarget{TargetTime: time.Now().Format("2006-01-02 15:04:04")}
	// 	closestBackupInfo, err := catalog.FindBackupInfo(recoveryTarget)
	// 	Expect(err).ToNot(HaveOccurred())
	// 	Expect(closestBackupInfo.ID).To(Equal("202101031200"))

	// 	recoveryTarget = &v1.RecoveryTarget{TargetTime: time.Date(2021, 1, 2, 12, 30, 0,
	// 		0, time.UTC).Format("2006-01-02 15:04:04")}
	// 	closestBackupInfo, err = catalog.FindBackupInfo(recoveryTarget)
	// 	Expect(err).ToNot(HaveOccurred())
	// 	Expect(closestBackupInfo.ID).To(Equal("202101021200"))
	// })

	// It("will return an empty result when the closest backup cannot be found", func() {
	// 	recoveryTarget := &v1.RecoveryTarget{TargetTime: time.Date(2019, 1, 2, 12, 30,
	// 		0, 0, time.UTC).Format("2006-01-02 15:04:04")}
	// 	closestBackupInfo, err := catalog.FindBackupInfo(recoveryTarget)
	// 	Expect(err).ToNot(HaveOccurred())
	// 	Expect(closestBackupInfo).To(BeNil())
	// })

	// It("can find the backup info when BackupID is provided", func() {
	// 	recoveryTarget := &v1.RecoveryTarget{TargetName: "recovery_point_1", BackupID: "202101021200"}
	// 	BackupInfo, err := catalog.FindBackupInfo(recoveryTarget)
	// 	Expect(err).ToNot(HaveOccurred())
	// 	Expect(BackupInfo.ID).To(Equal("202101021200"))

	// 	trueVal := true
	// 	recoveryTarget = &v1.RecoveryTarget{TargetImmediate: &trueVal, BackupID: "202101011200"}
	// 	BackupInfo, err = catalog.FindBackupInfo(recoveryTarget)
	// 	Expect(err).ToNot(HaveOccurred())
	// 	Expect(BackupInfo.ID).To(Equal("202101011200"))
	// })
})

// var _ = Describe("pgbackrest info --set parsing", func() {
// 	const barmanCloudShowOutput = `{
// 		"cloud":{
//             "backup_label": null,
//             "begin_offset": 40,
//             "begin_time": "Tue Jan 19 03:14:08 2038",
//             "begin_wal": "000000010000000000000002",
//             "begin_xlog": "0/2000028",
//             "compression": null,
//             "config_file": "/pgdata/location/postgresql.conf",
//             "copy_stats": null,
//             "deduplicated_size": null,
//             "end_offset": 184,
//             "end_time": "Tue Jan 19 04:14:08 2038",
//             "end_wal": "000000010000000000000004",
//             "end_xlog": "0/20000B8",
//             "error": null,
//             "hba_file": "/pgdata/location/pg_hba.conf",
//             "ident_file": "/pgdata/location/pg_ident.conf",
//             "included_files": null,
//             "mode": "concurrent",
//             "pgdata": "/pgdata/location",
//             "server_name": "main",
//             "size": null,
//             "snapshots_info": {
//                 "provider": "gcp",
//                 "provider_info": {
//                     "project": "test_project"
//                 },
//                 "snapshots": [
//                     {
//                         "mount": {
//                             "mount_options": "rw,noatime",
//                             "mount_point": "/opt/disk0"
//                         },
//                         "provider": {
//                             "device_name": "dev0",
//                             "snapshot_name": "snapshot0",
//                             "snapshot_project": "test_project"
//                         }
//                     },
//                     {
//                         "mount": {
//                             "mount_options": "rw",
//                             "mount_point": "/opt/disk1"
//                         },
//                         "provider": {
//                             "device_name": "dev1",
//                             "snapshot_name": "snapshot1",
//                             "snapshot_project": "test_project"
//                         }
//                     }
//                 ]
//             },
//             "status": "DONE",
//             "systemid": "6885668674852188181",
//             "tablespaces": [
//                 ["tbs1", 16387, "/fake/location"],
//                 ["tbs2", 16405, "/another/location"]
//             ],
//             "timeline": 1,
//             "version": 150000,
//             "xlog_segment_size": 16777216,
//             "backup_id": "20201020T115231"
//         }
// }`

// 	It("must parse a correct output", func() {
// 		result, err := NewSingleBackupCatalogFromPgbackrestInfo(barmanCloudShowOutput)
// 		Expect(err).ToNot(HaveOccurred())
// 		Expect(result).ToNot(BeNil())
// 		Expect(result.ID).To(Equal("20201020T115231"))
// 		Expect(result.SystemID).To(Equal("6885668674852188181"))
// 		Expect(result.BeginTimeString).To(Equal("Tue Jan 19 03:14:08 2038"))
// 		Expect(result.EndTimeString).To(Equal("Tue Jan 19 04:14:08 2038"))
// 		Expect(result.BeginTime).To(BeTemporally("==", time.Date(
// 			2038, 1, 19,
// 			3, 14, 8,
// 			0,
// 			time.UTC,
// 		)))
// 		Expect(result.EndTime).To(BeTemporally("==", time.Date(
// 			2038, 1, 19,
// 			4, 14, 8,
// 			0,
// 			time.UTC,
// 		)))
// 	})
// })
