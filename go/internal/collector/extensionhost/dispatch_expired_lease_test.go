// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestNextClaimedRefusesExpiredDispatchLease is the H1 regression proof for
// eshu-hq/eshu#11085: a dispatch-granted lease that is already past when the
// claim reaches the extension host must never reach the satellite process.
// Forking with an expired run context dies stillborn (context-deadline
// wall in centiseconds, launch_failure): the S0 shape of the 9 stillborn
// attempts on the issue. The refusal keeps the retryable launch_failure class so the
// claim plane retries the claim, but no process forks and the cause names
// the expired dispatch lease instead of a satellite failure.
func TestNextClaimedRefusesExpiredDispatchLease(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	// A granted lease already a second in the past at dispatch: stale
	// re-dispatch, a tiny live TTL, or a clock step between the dispatch
	// heartbeat and the launch.
	item.LeaseExpiresAt = testObservedAt().Add(-time.Second)
	runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
	recorder := &recordingStatusRecorder{}
	source := mustNewSource(t, runner, recorder)

	_, _, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want retryable launch_failure for an expired dispatch lease")
	}
	assertFailure(t, err, FailureClassLaunchFailure, false)
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("NextClaimed() error = %q, want it to name the expired dispatch lease", err.Error())
	}
	if got := len(runner.requests); got != 0 {
		t.Fatalf("satellite launches = %d, want 0: an expired dispatch lease must never fork the satellite", got)
	}
}

// TestNextClaimedLaunchesOnLiveDispatchLease pins the other side of the
// guard: a lease still live at dispatch reaches the satellite exactly once,
// so the refusal above cannot swallow healthy claims.
func TestNextClaimedLaunchesOnLiveDispatchLease(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	item.LeaseExpiresAt = testObservedAt().Add(time.Minute)
	runner := &recordingRunner{result: completeResult(item, testSDKFact(item))}
	recorder := &recordingStatusRecorder{}
	source := mustNewSource(t, runner, recorder)

	_, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil for a live dispatch lease", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true for a live dispatch lease")
	}
	if got := len(runner.requests); got != 1 {
		t.Fatalf("satellite launches = %d, want 1 for a live dispatch lease", got)
	}
}
