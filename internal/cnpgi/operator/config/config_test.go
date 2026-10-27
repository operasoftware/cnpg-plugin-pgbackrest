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

package config

import (
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"k8s.io/utils/ptr"

	"github.com/operasoftware/cnpg-plugin-pgbackrest/internal/cnpgi/metadata"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PluginConfiguration", func() {
	DescribeTable("Validate",
		func(configuration PluginConfiguration, valid bool) {
			err := configuration.Validate()
			if valid {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(err).To(HaveOccurred())
			}
		},
		Entry("archive only", PluginConfiguration{PgbackrestObjectName: "archive"}, true),
		Entry("recovery only", PluginConfiguration{RecoveryPgbackrestObjectName: "recovery"}, true),
		Entry("replica source only", PluginConfiguration{ReplicaSourcePgbackrestObjectName: "source"}, true),
		Entry("nothing referenced", PluginConfiguration{}, false),
	)

	It("accepts a replica-source-only cluster", func() {
		cluster := &cnpgv1.Cluster{
			Spec: cnpgv1.ClusterSpec{
				ReplicaCluster: &cnpgv1.ReplicaClusterConfiguration{
					Source:  "origin",
					Enabled: ptr.To(true),
				},
				ExternalClusters: []cnpgv1.ExternalCluster{
					{
						Name: "origin",
						PluginConfiguration: &cnpgv1.PluginConfiguration{
							Name:       metadata.PluginName,
							Parameters: map[string]string{"pgbackrestObjectName": "origin-archive"},
						},
					},
				},
			},
		}

		configuration := NewFromCluster(cluster)
		Expect(configuration.ReplicaSourcePgbackrestObjectName).To(Equal("origin-archive"))
		Expect(configuration.Validate()).To(Succeed())
	})
})
