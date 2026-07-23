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

package restorer

import (
	"errors"
	"fmt"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("walRestoreError", func() {
	const walName = "000000010000000000000001"

	exitError := func(code int) error {
		return exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	}

	It("returns ErrWALNotFound when archive-get reports the WAL is missing", func() {
		Expect(walRestoreError(walName, exitError(walNotFoundExitCode))).To(MatchError(ErrWALNotFound))
	})

	It("returns a generic error for other non-zero exit codes", func() {
		err := walRestoreError(walName, exitError(103))

		Expect(err).To(HaveOccurred())
		Expect(err).NotTo(MatchError(ErrWALNotFound))
		Expect(err.Error()).To(ContainSubstring("103"))
	})

	It("wraps failures that are not exit errors", func() {
		sentinel := errors.New("spawn failure")

		err := walRestoreError(walName, sentinel)

		Expect(err).To(MatchError(sentinel))
		Expect(err).NotTo(MatchError(ErrWALNotFound))
	})
})
