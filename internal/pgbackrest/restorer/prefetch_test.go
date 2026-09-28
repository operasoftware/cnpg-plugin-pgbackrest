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
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakePgbackrest stands in for "pgbackrest archive-get WAL DESTINATION". A WAL whose name
// contains "missing" is not in the archive (exit 1), and "gone" is the same after a second.
// One containing "slow" takes two seconds, written in two halves so a reader can catch it
// half done. Every call is appended to the calls file.
const fakePgbackrest = `#!/bin/sh
for last; do :; done
wal=$(eval echo "\${$(($# - 1))}")
echo "$wal" >> "$CALLS"
case "$wal" in
  *missing*) exit 1 ;;
  *gone*) sleep 1; exit 1 ;;
  *slow*) printf 'first half ' > "$last"; sleep 2; printf 'second half' >> "$last" ;;
  *) printf 'first half second half' > "$last" ;;
esac
`

var _ = Describe("WAL prefetch", func() {
	var (
		restorer *WALRestorer
		spoolDir string
		calls    string
	)

	callsFor := func(walName string) int {
		content, _ := os.ReadFile(calls)
		return strings.Count(string(content), walName+"\n")
	}

	BeforeEach(func(ctx SpecContext) {
		binDir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(binDir, "pgbackrest"), []byte(fakePgbackrest), 0o755)).To(Succeed()) //nolint:gosec
		GinkgoT().Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

		calls = filepath.Join(GinkgoT().TempDir(), "calls")
		spoolDir = GinkgoT().TempDir()
		var err error
		restorer, err = NewWALRestorer(ctx, []string{"CALLS=" + calls}, spoolDir)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			Eventually(func() int {
				downloads.Lock()
				defer downloads.Unlock()
				return len(downloads.running)
			}).WithTimeout(5 * time.Second).Should(BeZero())
		})
	})

	It("returns the requested WAL without waiting for the prefetch", func(ctx SpecContext) {
		destination := filepath.Join(GinkgoT().TempDir(), "RECOVERYXLOG")
		start := time.Now()
		started, err := restorer.RestoreWithPrefetch(ctx, "wal-1", destination, []string{"wal-2-slow"}, nil, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(started).To(Equal(1))
		Expect(time.Since(start)).To(BeNumerically("<", time.Second))
		Expect(destination).To(BeAnExistingFile())
	})

	It("reports a missing requested WAL without waiting for the prefetch", func(ctx SpecContext) {
		start := time.Now()
		_, err := restorer.RestoreWithPrefetch(
			ctx, "wal-1-missing", filepath.Join(GinkgoT().TempDir(), "RECOVERYXLOG"), []string{"wal-2-slow"}, nil, nil)
		Expect(err).To(MatchError(ErrWALNotFound))
		Expect(time.Since(start)).To(BeNumerically("<", time.Second))
	})

	It("puts a prefetched WAL in the spool only once it is complete", func(ctx SpecContext) {
		_, err := restorer.RestoreWithPrefetch(
			ctx, "wal-1", filepath.Join(GinkgoT().TempDir(), "RECOVERYXLOG"), []string{"wal-2-slow"}, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		Consistently(filepath.Join(spoolDir, "wal-2-slow")).WithTimeout(time.Second).ShouldNot(BeAnExistingFile())
		Eventually(func() string {
			content, _ := os.ReadFile(filepath.Join(spoolDir, "wal-2-slow"))
			return string(content)
		}).WithTimeout(5 * time.Second).Should(Equal("first half second half"))
	})

	It("waits for a WAL that is being prefetched instead of downloading it again", func(ctx SpecContext) {
		_, err := restorer.RestoreWithPrefetch(
			ctx, "wal-1", filepath.Join(GinkgoT().TempDir(), "RECOVERYXLOG"),
			[]string{"wal-2-slow", "wal-3-gone"}, nil, nil)
		Expect(err).NotTo(HaveOccurred())

		destination := filepath.Join(GinkgoT().TempDir(), "RECOVERYXLOG")
		_, err = restorer.RestoreFromSpool(ctx, "wal-3-gone", destination)
		Expect(err).To(MatchError(ErrWALNotFound))

		wasInSpool, err := restorer.RestoreFromSpool(ctx, "wal-2-slow", destination)
		Expect(err).NotTo(HaveOccurred())
		Expect(wasInSpool).To(BeTrue())
		Expect(os.ReadFile(destination)).To(BeEquivalentTo("first half second half"))
		Expect(callsFor("wal-2-slow")).To(Equal(1))
	})

	It("does not prefetch WAL files that are in the spool or being downloaded", func(ctx SpecContext) {
		Expect(os.WriteFile(filepath.Join(spoolDir, "wal-2"), []byte("spooled"), 0o600)).To(Succeed())
		destination := filepath.Join(GinkgoT().TempDir(), "RECOVERYXLOG")

		started, err := restorer.RestoreWithPrefetch(ctx, "wal-1", destination, []string{"wal-2", "wal-3-slow"}, nil, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(started).To(Equal(1))

		started, err = restorer.RestoreWithPrefetch(ctx, "wal-4", destination, []string{"wal-3-slow", "wal-5"}, nil, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(started).To(Equal(1))

		Eventually(func() int { return callsFor("wal-3-slow") + callsFor("wal-5") }).
			WithTimeout(5 * time.Second).Should(Equal(2))
		Expect(callsFor("wal-2")).To(BeZero())
	})
})
