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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AppendStandbyPrimaryOptions", func() {
	It("appends the primary as the second database", func() {
		options := AppendStandbyPrimaryOptions([]string{"backup"}, "cluster-pgbackrest", "/pg/data")

		Expect(strings.Join(options, " ")).To(Equal(
			"backup --pg2-host cluster-pgbackrest --pg2-host-type tls --pg2-host-port 8432" +
				" --pg2-host-cert-file /controller/certificates/streaming_replica.crt" +
				" --pg2-host-key-file /controller/certificates/streaming_replica.key" +
				" --pg2-host-ca-file /controller/certificates/server-ca.crt" +
				" --pg2-path /pg/data --pg2-user postgres --pg2-socket-path /controller/run/"))
	})
})

var _ = Describe("StandbyServerOptions", func() {
	const common = "server --tls-server-address 0.0.0.0 --tls-server-port 8432" +
		" --tls-server-cert-file /controller/certificates/server.crt" +
		" --tls-server-key-file /controller/certificates/server.key" +
		" --tls-server-ca-file /controller/certificates/client-ca.crt" +
		" --tls-server-auth streaming_replica=stanza"

	It("uses the given stderr log level", func() {
		Expect(strings.Join(StandbyServerOptions("streaming_replica", "stanza", "info"), " ")).To(Equal(
			common + " --log-level-stderr info --log-level-console off --log-level-file off"))
	})

	It("defaults the stderr log level to warn", func() {
		Expect(strings.Join(StandbyServerOptions("streaming_replica", "stanza", ""), " ")).To(Equal(
			common + " --log-level-stderr warn --log-level-console off --log-level-file off"))
	})

	It("authorises the given client common name", func() {
		Expect(StandbyServerOptions("custom-replica", "stanza", "")).
			To(ContainElements("--tls-server-auth", "custom-replica=stanza"))
	})
})

var _ = Describe("CertificateCN", func() {
	certificatePEM := func(commonName string) []byte {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		Expect(err).ToNot(HaveOccurred())
		template := &x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject:      pkix.Name{CommonName: commonName},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
		}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		Expect(err).ToNot(HaveOccurred())
		return pem.EncodeToMemory(&pem.Block{Type: pemCertificateType, Bytes: der})
	}

	It("returns the default common name", func() {
		Expect(CertificateCN(certificatePEM("streaming_replica"))).To(Equal("streaming_replica"))
	})

	It("returns a custom common name, skipping blocks that are not certificates", func() {
		data := append(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("key")}),
			certificatePEM("custom-replica")...)
		Expect(CertificateCN(data)).To(Equal("custom-replica"))
	})

	It("fails when the data holds no certificate", func() {
		_, err := CertificateCN([]byte("not a certificate"))
		Expect(err).To(MatchError(ContainSubstring("no PEM certificate found")))
	})

	It("fails when the certificate cannot be parsed", func() {
		_, err := CertificateCN(pem.EncodeToMemory(&pem.Block{Type: pemCertificateType, Bytes: []byte("garbage")}))
		Expect(err).To(MatchError(ContainSubstring("while parsing the certificate")))
	})

	It("fails when the common name is empty", func() {
		_, err := CertificateCN(certificatePEM(""))
		Expect(err).To(MatchError(ContainSubstring("empty common name")))
	})
})

var _ = DescribeTable("ValidateStandbyClientCN",
	func(clientCN, expectedError string) {
		err := ValidateStandbyClientCN(clientCN)
		if expectedError == "" {
			Expect(err).ToNot(HaveOccurred())
		} else {
			Expect(err).To(MatchError(ContainSubstring(expectedError)))
		}
	},
	Entry("accepts the default common name", "streaming_replica", ""),
	Entry("accepts a custom common name", "custom-replica.example", ""),
	Entry("rejects an empty common name", "", "is empty"),
	Entry("rejects a common name containing =", "a=b", `contains "="`),
)

var _ = DescribeTable("ValidateStandbyStanza",
	func(stanza, expectedError string) {
		err := ValidateStandbyStanza(stanza)
		if expectedError == "" {
			Expect(err).ToNot(HaveOccurred())
		} else {
			Expect(err).To(MatchError(ContainSubstring(expectedError)))
		}
	},
	Entry("accepts a cluster name", "cluster-example", ""),
	Entry("rejects an empty stanza", "", "is empty"),
	Entry("rejects a leading space", " cluster", "leading or trailing whitespace"),
	Entry("rejects a trailing tab", "cluster\t", "leading or trailing whitespace"),
	Entry("rejects *", "*", "every stanza"),
	Entry("rejects a list", "a,b", `contains ","`),
)
