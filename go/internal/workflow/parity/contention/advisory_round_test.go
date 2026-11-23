// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"testing"
	"time"
)

// fastAdvisoryHold keeps advisory items hidden for a short span in unit tests,
// so the drain after the window still finishes inside the test.
const fastAdvisoryHold = 300 * time.Millisecond

// TestAdvisoryRoundKeepsItsTargetsOpenAgainstDrainingWorkers pins the loaded
// -race ordering behind #11074. The guard admits a run only when its targets
// have no open item, so a late goroutine that reaches the guard after workers
// completed the first winner's items is rightly admitted too. The round must
// therefore keep its own targets open until every goroutine has passed the
// guard; the fake completes every visible admitted item at once, the way a
// worker would, so only a round that hides its items can pass.
func TestAdvisoryRoundKeepsItsTargetsOpenAgainstDrainingWorkers(t *testing.T) {
	opt := fastOptions()
	report, _ := runFake(t, faults{drainAdmitted: true}, false, true, opt)
	got := report.Criteria["advisory_admission"]
	if got.Status != StatusPass {
		t.Fatalf("advisory_admission = %+v, want PASS: a draining worker must not let a second goroutine win\n%s",
			got, report.Summary())
	}
}

// TestAdvisoryRoundVoidsWhenTheHoldEndsBeforeEveryGuardReturned pins the
// bound the hold actually gives. The hold keeps the round's targets open only
// until its deadline; a guard call that returns after that deadline may have
// evaluated its targets after the workers completed them, so the guard's
// verdict proves nothing about the product. Such a round is voided (counted
// and logged), never recorded as a duplicate admission: a stalled runner must
// not fail the criterion on the harness's own ordering (#11074).
func TestAdvisoryRoundVoidsWhenTheHoldEndsBeforeEveryGuardReturned(t *testing.T) {
	// A stalled runner (-race at GOMAXPROCS=2) can use the whole 1.5 s window
	// before one round finishes; that run judged nothing, so it is retried.
	for attempt := range 3 {
		opt := fastOptions()
		opt.AdvisoryHold = 5 * time.Millisecond
		report, _ := runFake(t, faults{drainAdmitted: true, admitDelay: 60 * time.Millisecond}, false, true, opt)
		if dups, _ := report.Advisory["duplicates"].([]string); len(dups) != 0 {
			t.Fatalf("advisory duplicates = %v, want none: a round whose hold ended before its guards returned proves nothing", dups)
		}
		rounds, _ := report.Advisory["rounds"].(int)
		voided, _ := report.Advisory["voided_rounds"].(int)
		if rounds != 0 {
			t.Fatalf("rounds judged = %d, want 0: every guard call outlasted the hold (%v)", rounds, report.Advisory)
		}
		if voided > 0 {
			return
		}
		t.Logf("attempt %d completed no round inside the window; retrying (%v)", attempt, report.Advisory)
	}
	t.Fatal("no round completed inside the window in 3 attempts, so none was voided")
}
