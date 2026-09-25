/*
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

package common

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	pgbackrestv1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DestinationCheck", func() {
	archive := func(uid string, generation int64) *pgbackrestv1.Archive {
		return &pgbackrestv1.Archive{ObjectMeta: metav1.ObjectMeta{
			Name: "archive", Namespace: "default", UID: types.UID("uid-" + uid), Generation: generation,
		}}
	}

	It("verifies only the destination of the last successful batch", func() {
		check := &DestinationCheck{}
		destination := destinationKey(archive("a", 1), "stanza")
		Expect(check.isVerified(destination)).To(BeFalse())

		check.markVerified(destination)
		Expect(check.isVerified(destination)).To(BeTrue())

		check.reset()
		Expect(check.isVerified(destination)).To(BeFalse())
	})

	It("treats any change of the Archive spec or of the stanza as a new destination", func() {
		check := &DestinationCheck{}
		check.markVerified(destinationKey(archive("a", 1), "stanza"))

		Expect(check.isVerified(destinationKey(archive("a", 2), "stanza"))).To(BeFalse())
		Expect(check.isVerified(destinationKey(archive("b", 1), "stanza"))).To(BeFalse())
		Expect(check.isVerified(destinationKey(archive("a", 1), "other"))).To(BeFalse())
	})

	It("checks every batch when it is nil", func() {
		var check *DestinationCheck
		destination := destinationKey(archive("a", 1), "stanza")
		check.markVerified(destination)
		check.reset()
		Expect(check.isVerified(destination)).To(BeFalse())
	})
})
