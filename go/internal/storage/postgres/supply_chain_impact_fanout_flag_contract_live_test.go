// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"strings"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// runsOf counts how many times scope ran in the handled list.
func runsOf(handled []string, scope string) int {
	runs := 0
	for _, entry := range handled {
		if strings.HasPrefix(entry, scope+"#") {
			runs++
		}
	}
	return runs
}

// TestFanOutMidRunFlagIsNeverClearedByTheFlaggedPassLive pins the production
// shape of the #9740 clear (#10972 reality audit, risk 7). An unrelated
// findings pass (a fleet scope holding only CVE-2026-9500) is claimed through
// ReducerQueue.Claim with no stamp, so its claim-time intent carries none. It
// is parked after its evidence load while the source's fan-out flags it through
// the in-flight arm (it was claimed before the source's load ended) and stamps
// {CVE-2026-9695}. Its own derived CVEs are disjoint from that stamp, but the
// handler compares against the claim-time stamp it never had, so the clear
// must not apply: its acknowledgement re-pends it, it runs exactly once more,
// and the queue settles. TestClearReplayFlagLive proves the store-level fence
// with a seen stamp; this test proves the handler never supplies one after a
// mid-run flag.
//
// Expected on main: GREEN (clearReplayFlag returns false with no claim-time
// stamp). A RED here means the handler cleared a flag it cannot have seen.
func TestFanOutMidRunFlagIsNeverClearedByTheFlaggedPassLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	c.seedFleetScope(0)
	unrelated := fleetScope(0)
	pass := c.startGatedPass(c.claimTarget(unrelated))
	c.rejectSource()
	if st := fanOutItem(t, c.ctx, c.db, pass.intent.IntentID); st.status != "claimed" || !st.flag || st.originSrc != convergenceSource {
		t.Fatalf("unrelated pass while parked = %+v, want claimed, flagged, stamped by %s", st, convergenceSource)
	}
	pass.finish()
	st := fanOutItem(t, c.ctx, c.db, pass.intent.IntentID)
	if st.status != "pending" || st.flag || !st.reopened {
		t.Fatalf("unrelated item after its ack = %+v, want re-pended (pending, flag consumed by the trigger, reopened_at set): the flagged pass must not clear its own mid-run flag", st)
	}
	c.handled = nil
	c.drain()
	if runs := runsOf(c.handled, unrelated); runs != 1 {
		t.Fatalf("unrelated scope reran %d times (handled %v), want exactly 1 replay then quiet", runs, c.handled)
	}
	if st := fanOutItem(t, c.ctx, c.db, pass.intent.IntentID); st.status != "succeeded" {
		t.Fatalf("unrelated item after the drain = %+v, want succeeded", st)
	}
}

// TestFanOutRowReadStillFlagsAHolderClaimedAfterTheSourceLoadLive pins the
// documented D4b residual as the current contract (#10972 reality audit,
// risk 5; HOLDER_FANOUT_INFLIGHT.md "The row read is unchanged"). The EPSS
// holder holds an active finding row for the CVE and is claimed after the
// source's evidence load ended. The in-flight arm narrows it away, but the row
// read finds its row and the reopen flags the claimed item anyway, so it reruns
// once after its own pass. If a later change narrows the row-read arm too, this
// test must change with the contract, not be deleted.
//
// Expected on main: GREEN.
func TestFanOutRowReadStillFlagsAHolderClaimedAfterTheSourceLoadLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	hook := c.hookFanOut()
	var holder reducercontract.Intent
	hook.afterLoadEnd = func() {
		c.rerun(convergenceHolder)
		holder = c.claimTarget(convergenceHolder)
	}
	result := c.rejectSource()
	if holder.IntentID == "" {
		t.Fatal("the source pass took no load-end read")
	}
	if hook.sourceLoadEndHolds(convergenceHolder) {
		t.Fatal("set A names the holder, want it claimed after the source's load ended")
	}
	if got := result.SubSignals["fanout_replay_flagged"]; got < 1 {
		t.Fatalf("source fanout_replay_flagged = %v, want the row-read arm to flag the claimed holder", got)
	}
	if st := fanOutItem(t, c.ctx, c.db, holder.IntentID); st.status != "claimed" || !st.flag {
		t.Fatalf("holder = %+v, want claimed with the replay bit (the documented row-read residual)", st)
	}
	// The flagged holder's pass loads after the change and still reruns once:
	// the cost of the residual. Then the queue settles on the right copy.
	c.finishClaimed(holder)
	if runs := c.drainedRuns(convergenceHolder); runs != 1 {
		t.Fatalf("holder reran %d times after its pass (handled %v), want exactly 1", runs, c.handled)
	}
	c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})
}
