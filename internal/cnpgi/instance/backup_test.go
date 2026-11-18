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
	"context"
	"net"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("checkBackupTarget", func() {
	It("runs on the primary regardless of the feature", func() {
		for _, enabled := range []bool{true, false} {
			standby, err := checkBackupTarget(enabled, "pod-1", "pod-1")
			Expect(err).ToNot(HaveOccurred())
			Expect(standby).To(BeFalse())
		}
	})

	It("runs on a standby when the feature is enabled", func() {
		standby, err := checkBackupTarget(true, "pod-1", "pod-2")
		Expect(err).ToNot(HaveOccurred())
		Expect(standby).To(BeTrue())
	})

	It("fails on a standby when the feature is disabled", func() {
		_, err := checkBackupTarget(false, "pod-1", "pod-2")
		Expect(err).To(MatchError(ContainSubstring("backupStandby is not enabled")))
	})

	It("fails on a standby when the cluster has no current primary", func() {
		_, err := checkBackupTarget(true, "", "pod-2")
		Expect(err).To(MatchError(ContainSubstring("no current primary")))
	})

	It("runs locally when the feature is disabled and the cluster has no current primary", func() {
		standby, err := checkBackupTarget(false, "", "pod-2")
		Expect(err).ToNot(HaveOccurred())
		Expect(standby).To(BeFalse())
	})
})

var _ = Describe("waitForStandbyServer", func() {
	const (
		timeout  = 2 * time.Second
		interval = 20 * time.Millisecond
	)

	// freeAddress returns an address nothing listens on
	freeAddress := func() string {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())
		address := listener.Addr().String()
		Expect(listener.Close()).To(Succeed())
		return address
	}

	listen := func(address string) {
		listener, err := net.Listen("tcp", address)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(listener.Close)
	}

	It("returns when the server is listening", func(ctx SpecContext) {
		address := freeAddress()
		listen(address)
		Expect(waitForStandbyServer(ctx, address, timeout, interval)).To(Succeed())
	})

	It("returns once a server starts listening", func(ctx SpecContext) {
		address := freeAddress()
		listeners := make(chan net.Listener, 1)
		time.AfterFunc(100*time.Millisecond, func() {
			listener, _ := net.Listen("tcp", address)
			listeners <- listener
		})

		Expect(waitForStandbyServer(ctx, address, timeout, interval)).To(Succeed())
		var listener net.Listener
		Eventually(listeners).Should(Receive(&listener))
		Expect(listener).ToNot(BeNil())
		Expect(listener.Close()).To(Succeed())
	})

	It("fails with the address when nothing listens", func(ctx SpecContext) {
		address := freeAddress()
		err := waitForStandbyServer(ctx, address, 100*time.Millisecond, interval)
		Expect(err).To(MatchError(ContainSubstring("not reachable at " + address)))
		Expect(err).To(MatchError(ContainSubstring("expose port 8432")))
	})

	It("reports that no attempt completed when the timeout is zero", func(ctx SpecContext) {
		err := waitForStandbyServer(ctx, freeAddress(), 0, interval)
		Expect(err).To(MatchError(ContainSubstring("no connection attempt completed")))
	})

	It("returns the cancellation of the parent context", func(ctx SpecContext) {
		parent, cancel := context.WithCancel(ctx)
		time.AfterFunc(100*time.Millisecond, cancel)
		DeferCleanup(cancel)

		err := waitForStandbyServer(parent, freeAddress(), timeout, interval)
		Expect(err).To(MatchError(context.Canceled))
	})
})
