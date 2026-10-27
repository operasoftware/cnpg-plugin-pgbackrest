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
	"github.com/cloudnative-pg/cnpg-i/pkg/identity"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("IdentityImplementation", func() {
	It("advertises WAL, backup and restore-job services", func(ctx SpecContext) {
		response, err := IdentityImplementation{}.GetPluginCapabilities(ctx, &identity.GetPluginCapabilitiesRequest{})
		Expect(err).NotTo(HaveOccurred())

		services := make([]identity.PluginCapability_Service_Type, 0, len(response.Capabilities))
		for _, capability := range response.Capabilities {
			services = append(services, capability.GetService().GetType())
		}
		Expect(services).To(ConsistOf(
			identity.PluginCapability_Service_TYPE_WAL_SERVICE,
			identity.PluginCapability_Service_TYPE_BACKUP_SERVICE,
			identity.PluginCapability_Service_TYPE_RESTORE_JOB,
		))
	})
})
