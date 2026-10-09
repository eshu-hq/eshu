// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/clock"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// TestIdentityCacheWaitBoundIsOneHeartbeatOfTheQueueLease pins the real wiring,
// not a constant against its own definition (#7805). The bound on how long a
// caller waits inside the identity epoch cache is the reducer's heartbeat
// interval. The work queue the reducer actually builds (configureReducerQueue)
// must carry a lease whose half is exactly reducerHeartbeatInterval, the value
// handed to the cache and the value the service heartbeats at. If someone
// changes the queue's lease without the heartbeat, or the reverse, this fails.
func TestIdentityCacheWaitBoundIsOneHeartbeatOfTheQueueLease(t *testing.T) {
	t.Parallel()

	queue := configureReducerQueue(
		nil,
		runtimecfg.RetryPolicyConfig{},
		nil,
		false,
		func(string) string { return "" },
		runtimecfg.GraphBackendNornicDB,
		clock.System(),
		nil,
		nil,
	)
	if got, want := queue.LeaseDuration/2, reducerHeartbeatInterval; got != want {
		t.Fatalf("queue lease / 2 = %v, want reducerHeartbeatInterval %v: the cache bound and the service heartbeat must derive from the queue's own lease", got, want)
	}
}
