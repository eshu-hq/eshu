// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"
)

// TestProjectorHeartbeatIntervalIsAThirdOfTheLeaseWithinBounds pins the
// heartbeat cadence the projector keeps a claim alive on: a third of the lease
// so two beats can be lost before it expires, never longer than a minute, a
// minute when the lease is unset or negative, and at least a second for a lease
// too short to divide.
func TestProjectorHeartbeatIntervalIsAThirdOfTheLeaseWithinBounds(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		lease time.Duration
		want  time.Duration
	}{
		"unset lease":     {0, time.Minute},
		"negative lease":  {-time.Second, time.Minute},
		"five minutes":    {5 * time.Minute, time.Minute},
		"thirty seconds":  {30 * time.Second, 10 * time.Second},
		"too short":       {2 * time.Nanosecond, time.Second},
		"exactly a third": {3 * time.Minute, time.Minute},
	} {
		if got := projectorHeartbeatInterval(tc.lease); got != tc.want {
			t.Errorf("%s: projectorHeartbeatInterval(%s) = %s, want %s", name, tc.lease, got, tc.want)
		}
	}
}
