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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Backup from standby options", func() {
	topology := StandbyBackupTopology{
		PrimaryHost:   "10.0.0.5",
		PrimaryPort:   8432,
		PrimaryPGData: "/pg/data",
		CertFile:      "/certs/tls.crt",
		KeyFile:       "/certs/tls.key",
		CAFile:        "/certs/ca.crt",
	}

	Describe("AppendStandbyPrimaryHostOptions", func() {
		It("adds the primary as a second pgBackRest host over TLS", func() {
			options := AppendStandbyPrimaryHostOptions(nil, topology)
			Expect(strings.Join(options, " ")).To(Equal(
				"--pg2-host 10.0.0.5 --pg2-host-type tls --pg2-host-port 8432 " +
					"--pg2-host-cert-file /certs/tls.crt --pg2-host-key-file /certs/tls.key " +
					"--pg2-host-ca-file /certs/ca.crt " +
					"--pg2-path /pg/data --pg2-user postgres --pg2-socket-path /controller/run/"))
		})

		It("preserves options already present", func() {
			options := AppendStandbyPrimaryHostOptions([]string{"backup"}, topology)
			Expect(options[0]).To(Equal("backup"))
			Expect(strings.Join(options, " ")).To(ContainSubstring("--pg2-host 10.0.0.5"))
		})
	})

	Describe("BackupStandbyOption", func() {
		It("formats the --backup-standby flag", func() {
			Expect(BackupStandbyOption()).To(Equal("--backup-standby=y"))
		})
	})

	Describe("PgbackrestServerOptions", func() {
		It("builds the pgbackrest TLS server invocation", func() {
			options := PgbackrestServerOptions(ServerConfig{
				Address:      "*",
				Port:         8432,
				PGData:       "/pg/data",
				CertFile:     "/srv/tls.crt",
				KeyFile:      "/srv/tls.key",
				CAFile:       "/srv/ca.crt",
				AuthClientCN: "pgbackrest-client",
			})
			Expect(strings.Join(options, " ")).To(Equal(
				"server --tls-server-address * --tls-server-port 8432 " +
					"--tls-server-cert-file /srv/tls.crt --tls-server-key-file /srv/tls.key " +
					"--tls-server-ca-file /srv/ca.crt --tls-server-auth pgbackrest-client=* " +
					"--pg1-path /pg/data --pg1-user postgres --pg1-socket-path /controller/run/ " +
					"--log-level-stderr warn --log-level-console off"))
		})
	})

	DescribeTable("ShouldConfigurePrimaryPeer",
		func(enabled bool, currentPrimary, instanceName string, expected bool) {
			Expect(ShouldConfigurePrimaryPeer(enabled, currentPrimary, instanceName)).To(Equal(expected))
		},
		Entry("disabled", false, "cluster-1", "cluster-2", false),
		Entry("enabled but no primary known yet", true, "", "cluster-2", false),
		Entry("enabled but this instance is the primary", true, "cluster-1", "cluster-1", false),
		Entry("enabled and this instance is a standby", true, "cluster-1", "cluster-2", true),
	)
})
