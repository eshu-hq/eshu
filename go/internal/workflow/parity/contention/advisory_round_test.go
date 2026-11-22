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
