// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

var trackerT0 = time.Date(2026, time.November, 2, 18, 0, 0, 0, time.UTC)

func at(seconds float64) time.Time {
	return trackerT0.Add(time.Duration(seconds * float64(time.Second)))
}

func grant(item string, token int64) (workflow.WorkItem, workflow.Claim) {
	return workflow.WorkItem{WorkItemID: item, RunID: Prefix + "run-1-0", ScopeID: Prefix + "scope-0"},
		workflow.Claim{ClaimID: Prefix + "claim-x", WorkItemID: item, FencingToken: token}
}

func newTestTracker() *tracker { return newTracker(60*time.Second, 2*time.Second) }

func mustTrigger(t *testing.T, v *Violation, want Trigger) {
	t.Helper()
	if v == nil {
		t.Fatalf("no violation, want %s", want)
	}
	if v.Trigger != want {
		t.Fatalf("trigger = %s (%s), want %s", v.Trigger, v.Detail, want)
	}
}

func mustClean(t *testing.T, v *Violation) {
	t.Helper()
	if v != nil {
		t.Fatalf("unexpected violation %s: %s", v.Trigger, v.Detail)
	}
}

func TestTrackerAcceptsReclaimAfterLeaseExpiry(t *testing.T) {
	tr := newTestTracker()
	item := Prefix + "wi-1-0"
	w, c := grant(item, 1)
	mustClean(t, tr.onGrant(w, c, at(0), at(0.01)))
	w, c = grant(item, 2)
	mustClean(t, tr.onGrant(w, c, at(70), at(70.01)))
}

func TestTrackerDualClaimWhileLeaseLive(t *testing.T) {
	tr := newTestTracker()
	item := Prefix + "wi-1-0"
	w, c := grant(item, 1)
	mustClean(t, tr.onGrant(w, c, at(0), at(0.01)))
	tr.onAbandon(item, 1)
	w, c = grant(item, 2)
	mustTrigger(t, tr.onGrant(w, c, at(10), at(10.01)), TriggerDualClaim)
}

// TestTrackerDualClaimFoundAtLaterEnd: the earlier holder's ending call left
// only after the second grant arrived, so both held the item at once.
func TestTrackerDualClaimFoundAtLaterEnd(t *testing.T) {
	tr := newTestTracker()
	item := Prefix + "wi-1-0"
	w, c := grant(item, 1)
	mustClean(t, tr.onGrant(w, c, at(0), at(0.01)))
	w, c = grant(item, 2)
	mustClean(t, tr.onGrant(w, c, at(10), at(10.01)))
	mustTrigger(t, tr.onEnd(item, 1, endRelease, at(20), at(20.01)), TriggerDualClaim)
}

// TestTrackerLateEndAckIsNotDualClaim: the release left before the second
// grant arrived, even though its ack was recorded after it.
func TestTrackerLateEndAckIsNotDualClaim(t *testing.T) {
	tr := newTestTracker()
	item := Prefix + "wi-1-0"
	w, c := grant(item, 1)
	mustClean(t, tr.onGrant(w, c, at(0), at(0.01)))
	w, c = grant(item, 2)
	mustClean(t, tr.onGrant(w, c, at(1.5), at(1.6)))
	mustClean(t, tr.onEnd(item, 1, endRelease, at(1), at(1.7)))
}

func TestTrackerRenewExtendsLease(t *testing.T) {
	tr := newTestTracker()
	item := Prefix + "wi-1-0"
	w, c := grant(item, 1)
	mustClean(t, tr.onGrant(w, c, at(0), at(0.01)))
	tr.onRenew(item, 1, at(50))
	tr.onAbandon(item, 1)
	w, c = grant(item, 2)
	mustTrigger(t, tr.onGrant(w, c, at(70), at(70.01)), TriggerDualClaim)
}

func TestTrackerReleaseFreesItem(t *testing.T) {
	tr := newTestTracker()
	item := Prefix + "wi-1-0"
	w, c := grant(item, 1)
	mustClean(t, tr.onGrant(w, c, at(0), at(0.01)))
	mustClean(t, tr.onEnd(item, 1, endRelease, at(1), at(1.01)))
	w, c = grant(item, 2)
	mustClean(t, tr.onGrant(w, c, at(2), at(2.01)))
}

func TestTrackerFencingRegression(t *testing.T) {
	tr := newTestTracker()
	item := Prefix + "wi-1-0"
	w, c := grant(item, 3)
	mustClean(t, tr.onGrant(w, c, at(0), at(0.01)))
	mustClean(t, tr.onEnd(item, 3, endRelease, at(1), at(1.01)))
	w, c = grant(item, 3)
	mustTrigger(t, tr.onGrant(w, c, at(2), at(2.01)), TriggerFencingRegression)
	tr2 := newTestTracker()
	w, c = grant(item, 3)
	mustClean(t, tr2.onGrant(w, c, at(0), at(0.01)))
	mustClean(t, tr2.onEnd(item, 3, endRelease, at(1), at(1.01)))
	w, c = grant(item, 2)
	mustTrigger(t, tr2.onGrant(w, c, at(2), at(2.01)), TriggerFencingRegression)
}

// TestTrackerConcurrentResponsesDoNotFalseRegress: a grant whose response
// arrives after a later request started cannot order against it.
func TestTrackerConcurrentResponsesDoNotFalseRegress(t *testing.T) {
	tr := newTestTracker()
	item := Prefix + "wi-1-0"
	w, c := grant(item, 2)
	mustClean(t, tr.onGrant(w, c, at(70), at(70.5)))
	w, c = grant(item, 1)
	// Request for token 1 started at 0 and its response landed at 71, after
	// token 2's request began: no happens-before edge, so no regression.
	mustClean(t, tr.onGrant(w, c, at(0), at(71)))
}

func TestTrackerExactlyOnce(t *testing.T) {
	tr := newTestTracker()
	item := Prefix + "wi-1-0"
	w, c := grant(item, 1)
	mustClean(t, tr.onGrant(w, c, at(0), at(0.01)))
	mustClean(t, tr.onEnd(item, 1, endComplete, at(1), at(1.01)))
	mustClean(t, tr.onEnd(item, 1, endComplete, at(2), at(2.01))) // identical replay
	w, c = grant(item, 2)
	mustTrigger(t, tr.onGrant(w, c, at(3), at(3.01)), TriggerExactlyOnce)
	mustTrigger(t, tr.onEnd(item, 2, endComplete, at(4), at(4.01)), TriggerExactlyOnce)
}

func TestTrackerNonSyntheticGrant(t *testing.T) {
	tr := newTestTracker()
	w, c := grant("real-item", 1)
	mustTrigger(t, tr.onGrant(w, c, at(0), at(0.01)), TriggerNonSynthetic)
	w, c = grant(Prefix+"wi-1-0", 1)
	w.ScopeID = "scope-real"
	mustTrigger(t, tr.onGrant(w, c, at(0), at(0.01)), TriggerNonSynthetic)
}

func TestTrackerNewerGrantForStaleWriter(t *testing.T) {
	tr := newTestTracker()
	item := Prefix + "wi-1-0"
	w, c := grant(item, 1)
	mustClean(t, tr.onGrant(w, c, at(0), at(0.01)))
	if tr.hasNewerGrant(item, 1) {
		t.Fatalf("newer grant reported before any re-claim")
	}
	w, c = grant(item, 2)
	mustClean(t, tr.onGrant(w, c, at(80), at(80.01)))
	if !tr.hasNewerGrant(item, 1) {
		t.Fatalf("newer grant not reported after re-claim")
	}
}

func TestTrackerIsRaceSafe(t *testing.T) {
	tr := newTestTracker()
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for n := range 200 {
				item := Prefix + "wi-race-" + string(rune('a'+g)) + "-" + time.Duration(n).String()
				w, c := grant(item, 1)
				_ = tr.onGrant(w, c, at(0), at(0.01))
				_ = tr.onEnd(item, 1, endComplete, at(1), at(1.01))
			}
		}(g)
	}
	wg.Wait()
	if got := tr.terminalCount(); got != 1600 {
		t.Fatalf("terminal count = %d, want 1600", got)
	}
}

// TestTrackerRejectedLiveHolderIsDualClaim: the holder's own write was
// fenced away inside its lease, and another grant landed inside that lease.
func TestTrackerRejectedLiveHolderIsDualClaim(t *testing.T) {
	tr := newTestTracker()
	item := Prefix + "wi-1-0"
	w, c := grant(item, 1)
	mustClean(t, tr.onGrant(w, c, at(0), at(0.01)))
	w, c = grant(item, 2)
	mustClean(t, tr.onGrant(w, c, at(1), at(1.01)))
	mustTrigger(t, tr.onRejected(item, 1, at(2)), TriggerDualClaim)
}

// TestTrackerRejectedAfterLeaseIsNotDualClaim: a rejection after the lease
// ran out is the normal reap path, not a dual claim.
func TestTrackerRejectedAfterLeaseIsNotDualClaim(t *testing.T) {
	tr := newTestTracker()
	item := Prefix + "wi-1-0"
	w, c := grant(item, 1)
	mustClean(t, tr.onGrant(w, c, at(0), at(0.01)))
	w, c = grant(item, 2)
	mustClean(t, tr.onGrant(w, c, at(70), at(70.01)))
	mustClean(t, tr.onRejected(item, 1, at(71)))
}
