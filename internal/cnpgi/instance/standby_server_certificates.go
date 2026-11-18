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

package instance

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	pgbackrestCommand "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/command"
)

// certificateSnapshot is what the local files decide.
type certificateSnapshot struct {
	fingerprint string
	clientCN    string
}

// readCertificates fails while the certificates are missing or inconsistent. The
// fingerprint is a hash of the content of the four files.
func (s *StandbyServerRunnable) readCertificates() (certificateSnapshot, error) {
	hash := sha256.New()
	read := func(file string) ([]byte, error) {
		data, err := os.ReadFile(file) // #nosec G304
		if err != nil {
			return nil, err
		}
		_, _ = fmt.Fprintf(hash, "%s:%d:", file, len(data))
		_, _ = hash.Write(data)
		return data, nil
	}

	certificate, err := read(orDefault(s.CertFile, postgres.ServerCertificateLocation))
	if err != nil {
		return certificateSnapshot{}, err
	}
	key, err := read(orDefault(s.KeyFile, postgres.ServerKeyLocation))
	if err != nil {
		return certificateSnapshot{}, err
	}
	if _, err := tls.X509KeyPair(certificate, key); err != nil {
		return certificateSnapshot{}, fmt.Errorf("while loading the server certificate and key: %w", err)
	}

	caFile := orDefault(s.CAFile, postgres.ClientCACertificateLocation)
	ca, err := read(caFile)
	if err != nil {
		return certificateSnapshot{}, err
	}
	if !x509.NewCertPool().AppendCertsFromPEM(ca) {
		return certificateSnapshot{}, fmt.Errorf("no certificate found in %s", caFile)
	}

	replicationCertFile := orDefault(s.ReplicationCertFile, postgres.StreamingReplicaCertificateLocation)
	replicationCert, err := read(replicationCertFile)
	if err != nil {
		return certificateSnapshot{}, err
	}
	clientCN, err := pgbackrestCommand.CertificateCN(replicationCert)
	if err != nil {
		return certificateSnapshot{}, fmt.Errorf(
			"while reading the replication certificate %s: %w", replicationCertFile, err)
	}
	if err := pgbackrestCommand.ValidateStandbyClientCN(clientCN); err != nil {
		return certificateSnapshot{}, err
	}

	return certificateSnapshot{fingerprint: hex.EncodeToString(hash.Sum(nil)), clientCN: clientCN}, nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
