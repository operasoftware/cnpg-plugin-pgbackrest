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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"
	pgbackrestApi "github.com/operasoftware/cnpg-plugin-pgbackrest/internal/pgbackrest/api"
)

var _ = Describe("backupResultMetadata", func() {
	oneRepository := &pgbackrestApi.PgbackrestConfiguration{
		Repositories: []pgbackrestApi.PgbackrestRepository{
			{EndpointURL: "https://s3.example.com", Bucket: "bucket", DestinationPath: "/path"},
		},
	}

	It("serializes the cluster UID, the stanza, the plugin identity, the static plugin data and the repositories", func() {
		m := newBackupResultMetadata(types.UID("uid-1234"), "cluster-example", oneRepository).toMap()

		Expect(m).To(Equal(map[string]string{
			"version":      metadata.Data.Version,
			"name":         metadata.Data.Name,
			"displayName":  metadata.Data.DisplayName,
			"clusterUID":   "uid-1234",
			"pluginName":   metadata.PluginName,
			"stanza":       "cluster-example",
			"repositories": "https://s3.example.com/bucket/path",
		}))
	})

	It("renders two repositories in configuration order, joined by a comma", func() {
		configuration := &pgbackrestApi.PgbackrestConfiguration{
			Repositories: []pgbackrestApi.PgbackrestRepository{
				{EndpointURL: "https://a", Bucket: "b1", DestinationPath: "/p1"},
				{EndpointURL: "https://c", Bucket: "b2", DestinationPath: "/p2"},
			},
		}

		Expect(describeRepositories(configuration)).To(Equal("https://a/b1/p1,https://c/b2/p2"))
	})

	It("round-trips through a map", func() {
		original := newBackupResultMetadata(types.UID("uid-1234"), "cluster-example", oneRepository)

		Expect(newBackupResultMetadataFromMap(original.toMap())).To(Equal(original))
	})

	It("returns empty metadata for a nil map", func() {
		Expect(newBackupResultMetadataFromMap(nil)).To(Equal(backupResultMetadata{}))
	})

	It("ignores unknown keys and tolerates missing ones", func() {
		m := newBackupResultMetadataFromMap(map[string]string{
			"version": "0.1.0",
			"foo":     "bar",
		})

		Expect(m.version).To(Equal("0.1.0"))
		Expect(m.clusterUID).To(BeEmpty())
		Expect(m.pluginName).To(BeEmpty())
		Expect(m.stanza).To(BeEmpty())
		Expect(m.repositories).To(BeEmpty())
	})
})
