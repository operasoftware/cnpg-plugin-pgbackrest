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
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/utils"
)

// StandbyServerPort is the port of the pgbackrest TLS server that runs next to the primary
const StandbyServerPort = 8432

const pemCertificateType = "CERTIFICATE"

// AppendStandbyPrimaryOptions appends the options describing the primary as the
// second pgbackrest database, reached through its TLS server at the given host
func AppendStandbyPrimaryOptions(options []string, host, pgDataDirectory string) []string {
	return append(
		options,
		utils.FormatDbFlag(1, "host"), host,
		utils.FormatDbFlag(1, "host-type"), "tls",
		utils.FormatDbFlag(1, "host-port"), strconv.Itoa(StandbyServerPort),
		utils.FormatDbFlag(1, "host-cert-file"), postgres.StreamingReplicaCertificateLocation,
		utils.FormatDbFlag(1, "host-key-file"), postgres.StreamingReplicaKeyLocation,
		utils.FormatDbFlag(1, "host-ca-file"), postgres.ServerCACertificateLocation,
		utils.FormatDbFlag(1, "path"), pgDataDirectory,
		utils.FormatDbFlag(1, "user"), "postgres",
		utils.FormatDbFlag(1, "socket-path"), "/controller/run/",
	)
}

// StandbyServerOptions returns the pgbackrest options running the TLS server
// that lets standbys of the cluster back up through this instance
func StandbyServerOptions(clientCN, stanza, logLevelStderr string) []string {
	if logLevelStderr == "" {
		logLevelStderr = "warn"
	}

	return []string{
		"server",
		"--tls-server-address", "0.0.0.0",
		"--tls-server-port", strconv.Itoa(StandbyServerPort),
		"--tls-server-cert-file", postgres.ServerCertificateLocation,
		"--tls-server-key-file", postgres.ServerKeyLocation,
		"--tls-server-ca-file", postgres.ClientCACertificateLocation,
		"--tls-server-auth", fmt.Sprintf("%s=%s", clientCN, stanza),
		"--log-level-stderr", logLevelStderr,
		"--log-level-console", "off",
		"--log-level-file", "off",
	}
}

// ValidateStandbyClientCN rejects a client common name that pgBackRest cannot
// express in --tls-server-auth, which splits the option at its first "="
func ValidateStandbyClientCN(clientCN string) error {
	if clientCN == "" {
		return errors.New("the common name of the replication certificate is empty")
	}
	if strings.Contains(clientCN, "=") {
		return fmt.Errorf(
			"the common name %q of the replication certificate contains \"=\", "+
				"which the pgBackRest TLS server cannot authorise", clientCN)
	}
	return nil
}

// ValidateStandbyStanza rejects a stanza that would make --tls-server-auth authorise
// something else: its value is a comma-separated list where "*" means every stanza
func ValidateStandbyStanza(stanza string) error {
	switch {
	case stanza == "":
		return errors.New("the stanza is empty")
	case stanza != strings.TrimSpace(stanza):
		return fmt.Errorf("the stanza %q has leading or trailing whitespace, "+
			"which the pgBackRest TLS server cannot authorise", stanza)
	case stanza == "*":
		return errors.New("the stanza \"*\" would let the pgBackRest TLS server authorise every stanza")
	case strings.Contains(stanza, ","):
		return fmt.Errorf("the stanza %q contains \",\", "+
			"which the pgBackRest TLS server reads as a list of stanzas", stanza)
	}
	return nil
}

// CertificateCN returns the common name of the first certificate in the PEM data,
// the identity the TLS server authorises
func CertificateCN(data []byte) (string, error) {
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return "", errors.New("no PEM certificate found")
		}
		if block.Type != pemCertificateType {
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("while parsing the certificate: %w", err)
		}
		if cert.Subject.CommonName == "" {
			return "", errors.New("the certificate has an empty common name")
		}
		return cert.Subject.CommonName, nil
	}
}
