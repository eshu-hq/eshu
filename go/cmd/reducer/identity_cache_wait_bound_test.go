// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"
)

// TestIdentityCacheWaitBoundIsOneHeartbeatOfTheClaimLease pins that the bound on
// how long a waiter blocks inside the identity epoch cache is the reducer's
// heartbeat interval, derived from the same claim lease the work queue uses
// (#7805). The cache adds no knob: changing the lease moves both together.
func TestIdentityCacheWaitBoundIsOneHeartbeatOfTheClaimLease(t *testing.T) {
	t.Parallel()

	if got, want := reducerHeartbeatInterval, reducerClaimLeaseDuration/2; got != want {
		t.Fatalf("reducerHeartbeatInterval = %v, want half of the claim lease %v", got, want)
	}
	if reducerClaimLeaseDuration != time.Minute {
		t.Fatalf("claim lease = %v, want the one-minute lease the queue is built with", reducerClaimLeaseDuration)
	}
}
