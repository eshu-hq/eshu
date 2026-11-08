// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"testing"
	"time"
)

// 5.2 lag metric for eshu-hq/eshu#10354: lag is now minus the cursor
// fetchTime, classified against the freshness window. Operators read it
// through the source freshness state (State.FreshnessState); the cadence
// check is lag within 10-15 min (mirror ADR line 79).
func TestCVEListMirrorCursorLag(t *testing.T) {
	t.Parallel()

	cur := CVEListMirrorCursor{FetchTime: "2026-11-07T23:29:09.281Z", BaselineDate: "2026-11-07"}
	now, _ := time.Parse(time.RFC3339Nano, "2026-11-07T23:39:09.281Z")

	lag, state, err := cur.Lag(now, 15*time.Minute)
	if err != nil {
		t.Fatalf("Lag() err = %v", err)
	}
	if lag != 10*time.Minute {
		t.Fatalf("Lag() = %v, want 10m (now minus cursor fetchTime)", lag)
	}
	if state != FreshnessFresh {
		t.Fatalf("Lag() state = %q, want fresh inside the window", state)
	}

	staleAt, _ := time.Parse(time.RFC3339Nano, "2026-11-07T23:50:09.281Z")
	if _, state, err := cur.Lag(staleAt, 15*time.Minute); err != nil || state != FreshnessStale {
		t.Fatalf("Lag() beyond window = (%q, %v), want (stale, nil)", state, err)
	}

	bad := CVEListMirrorCursor{FetchTime: "not-a-time"}
	if _, _, err := bad.Lag(now, 15*time.Minute); err == nil {
		t.Fatal("Lag() err = nil for unparseable cursor fetchTime, want failure")
	}
}
