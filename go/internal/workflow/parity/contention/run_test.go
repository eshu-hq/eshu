// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fastSpec is a sub-second lease model so the fake runs the whole §5 mix,
// reaping included, inside a unit test.
func fastSpec() Spec {
	return Spec{
		Target: TargetLocal, Digest: "sha256:fake", WorkerCounts: 3, WorkerMultiple: 1,
		LeaseTTL: 400 * time.Millisecond, HeartbeatInterval: 120 * time.Millisecond, ReapInterval: 100 * time.Millisecond,
		BacklogItems: 240, BacklogScopes: 12, QuotaBudget: 4,
		Window: 1500 * time.Millisecond, WallClockCap: 30 * time.Second,
		StarvationBound: 5 * time.Second, LockWaitBound: time.Second,
		Headroom: Headroom{MaxConnections: 80, MaxLockWaiters: 5}, Seed: 7,
		Fields: map[string]string{},
	}
}

func fastOptions() Options {
	mix := DefaultMix()
	mix.HeartbeatSkip, mix.LongHold, mix.Replay = 0.10, 0.05, 0.20
	return Options{
		SampleInterval: 20 * time.Millisecond, CensusEvery: 2, FencingEvery: 2,
		IdlePoll: 5 * time.Millisecond, RequeueDelay: 20 * time.Millisecond,
		AdvisoryInterval: 250 * time.Millisecond, DrainTimeout: 20 * time.Second, Mix: mix,
	}
}

func runFake(t *testing.T, f faults, pin, advisory bool, opt Options) (*Report, *fakeLeg) {
	t.Helper()
	leg := newFakeLeg(f, pin, 4)
	name := "in-tree"
	if pin {
		name = "satellite"
	}
	report, err := Run(context.Background(), fastSpec(), Leg{
		Name: name, Backend: leg, Observer: leg, Pin: pin, Advisory: advisory, Quota: true,
		HarnessReaper: true, ServerLatency: "fake", DatabaseName: "fake",
	}, opt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return report, leg
}

// assertCleanedUp checks the reversal ran on every exit path: no
// synthetic row remains and the residue was measured.
func assertCleanedUp(t *testing.T, r *Report, leg *fakeLeg) {
	t.Helper()
	census, _ := leg.Census(context.Background())
	if census.prefixedTotal() != 0 || r.Residue.FirstZero != 0 {
		t.Fatalf("synthetic rows left after reversal: fake=%d report=%d", census.prefixedTotal(), r.Residue.FirstZero)
	}
	if r.Phases.End.IsZero() || (r.Deleted == nil && r.CleanupSkipped == "") {
		t.Fatalf("reversal did not run: %+v", r.Phases)
	}
}

func TestRunCleanFakeLegsPassEveryCriterion(t *testing.T) {
	for _, pin := range []bool{false, true} {
		report, leg := runFake(t, faults{}, pin, !pin, fastOptions())
		if report.Verdict != VerdictClean {
			t.Fatalf("pin=%v verdict %s\n%s", pin, report.Verdict, report.Summary())
		}
		assertCleanedUp(t, report, leg)
		// At most the extra items stay held; fewer only when the run
		// crossed an hour and a new quota bucket opened.
		if report.QuotaHeld > max(2, 4/2) {
			t.Fatalf("quota held %d, want at most %d", report.QuotaHeld, max(2, 4/2))
		}
		if pin && report.Criteria["advisory_admission"].Status != StatusNotApplicable {
			t.Fatalf("satellite advisory = %+v, want NOT_APPLICABLE", report.Criteria["advisory_admission"])
		}
		if !pin && report.Criteria["pin"].Status != StatusNotApplicable {
			t.Fatalf("in-tree pin = %+v, want NOT_APPLICABLE", report.Criteria["pin"])
		}
		if _, err := report.JSON(); err != nil {
			t.Fatalf("report JSON: %v", err)
		}
	}
}

// Seeded-violation RED/GREEN pairs: each fake plants one §7 grant trigger;
// the clean run above is the GREEN half. Every abort must also run the
// reversal.
func TestRunAbortsOnSeededViolations(t *testing.T) {
	cases := []struct {
		name    string
		f       faults
		want    []Trigger
		sampler bool // only the database sampler can see it: it must fire mid-run
	}{
		{"double claim", faults{doubleClaimAt: 40}, []Trigger{TriggerDualClaim}, false},
		{"token regression", faults{regressTokenAt: 1}, []Trigger{TriggerFencingRegression}, false},
		{"lost item", faults{loseItemAt: 30}, []Trigger{TriggerLostItem}, true},
		{"non-synthetic grant", faults{grantRealAt: 25}, []Trigger{TriggerNonSynthetic}, false},
		{"non-synthetic row touched", faults{touchRealAt: 25}, []Trigger{TriggerNonSynthetic}, true},
		{"stale write accepted", faults{acceptStale: true}, []Trigger{TriggerStaleWriteAccept, TriggerExactlyOnce}, false},
		{"double terminal row", faults{terminalTwice: 20}, []Trigger{TriggerExactlyOnce}, true},
		// Database-only and client-only variants: each is visible to exactly
		// one detector, so each detector has its own RED case.
		{"server-side dual claim", faults{dbDualAt: 20}, []Trigger{TriggerDualClaim}, true},
		{"stale heartbeat accepted", faults{staleBeatOK: true}, []Trigger{TriggerStaleWriteAccept}, false},
		{"queue grows past plan", faults{growAt: 20}, []Trigger{TriggerQueueDepth}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report, leg := runFake(t, tc.f, true, false, fastOptions())
			if report.Verdict != VerdictAborted || report.Abort == nil {
				t.Fatalf("verdict %s, want ABORTED\n%s", report.Verdict, report.Summary())
			}
			ok := false
			for _, want := range tc.want {
				ok = ok || report.Abort.Trigger == want
			}
			t.Logf("abort %s: %s", report.Abort.Trigger, report.Abort.Detail)
			if !ok {
				t.Fatalf("abort trigger %s (%s), want one of %v", report.Abort.Trigger, report.Abort.Detail, tc.want)
			}
			if tc.sampler && (report.Abort.Source != SourceSampler || !report.Abort.At.Before(report.Phases.WorkEnd)) {
				t.Fatalf("abort from %q at %s (work end %s); want the sampler, mid-run",
					report.Abort.Source, report.Abort.At, report.Phases.WorkEnd)
			}
			assertCleanedUp(t, report, leg)
		})
	}
}

func TestRunAbortsOnExternalAbortFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "abort")
	if err := os.WriteFile(path, []byte("cpu 10500m > 10000m"), 0o600); err != nil {
		t.Fatal(err)
	}
	opt := fastOptions()
	opt.AbortFile = path
	report, leg := runFake(t, faults{}, false, true, opt)
	if report.Abort == nil || report.Abort.Trigger != TriggerExternal {
		t.Fatalf("abort = %+v, want %s", report.Abort, TriggerExternal)
	}
	assertCleanedUp(t, report, leg)
}

func TestRunAbortsOnWallClockCap(t *testing.T) {
	leg := newFakeLeg(faults{}, false, 4)
	spec := fastSpec()
	spec.WallClockCap = 800 * time.Millisecond
	report, err := Run(context.Background(), spec, Leg{
		Name: "in-tree", Backend: leg, Observer: leg,
		Quota: true, HarnessReaper: true,
	}, fastOptions())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Abort == nil || report.Abort.Trigger != TriggerWallClock {
		t.Fatalf("abort = %+v, want %s", report.Abort, TriggerWallClock)
	}
	assertCleanedUp(t, report, leg)
}

func TestRunWaitsForPostSeedAnalyze(t *testing.T) {
	report, _ := runFake(t, faults{}, false, true, fastOptions())
	if !report.Phases.StatsReady {
		t.Fatalf("stats_ready false although the fake reports a fresh ANALYZE")
	}
	leg := newFakeLeg(faults{}, false, 4)
	leg.coldStats = true
	opt := fastOptions()
	opt.StatsWait = 300 * time.Millisecond
	cold, err := Run(context.Background(), fastSpec(), Leg{
		Name: "in-tree", Backend: leg, Observer: leg,
		Quota: true, HarnessReaper: true, Advisory: true,
	}, opt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cold.Phases.StatsReady || cold.Phases.StatsWaitSeconds < 0.3 {
		t.Fatalf("cold stats: ready=%v waited %.2fs, want not ready after the 0.3s bound",
			cold.Phases.StatsReady, cold.Phases.StatsWaitSeconds)
	}
}

func TestRunRefusesNonEmptyBaseline(t *testing.T) {
	leg := newFakeLeg(faults{}, false, 4)
	// A row in this seed's own id family that this run did not create.
	leftover := Prefix + "wi-7-leftover"
	leg.items[leftover] = &fakeItem{item: workflowItemID(leftover)}
	report, err := Run(context.Background(), fastSpec(), Leg{Name: "in-tree", Backend: leg, Observer: leg}, fastOptions())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Abort == nil || report.Abort.Trigger != TriggerResidue {
		t.Fatalf("abort = %+v, want %s", report.Abort, TriggerResidue)
	}
	// The reversal must not delete rows it did not create.
	if _, ok := leg.items[leftover]; !ok || len(report.Deleted) != 0 || report.CleanupSkipped == "" {
		t.Fatalf("baseline abort deleted rows: leftover kept=%v deleted=%v skipped=%q", ok, report.Deleted, report.CleanupSkipped)
	}
}
