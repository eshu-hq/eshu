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

func fakeLegFor(leg *fakeLeg, head HeadroomSource, live bool) Leg {
	return Leg{
		Name: "in-tree", Backend: leg, Observer: leg, Headroom: head, Advisory: true, Quota: true,
		HarnessReaper: !live, ServerLatency: "fake", DatabaseName: "fake",
	}
}

func mustAbortBeforeWindow(t *testing.T, report *Report, want Trigger) {
	t.Helper()
	if report.Abort == nil || report.Abort.Trigger != want {
		t.Fatalf("abort = %+v, want %s", report.Abort, want)
	}
	if !report.Phases.WindowStart.IsZero() {
		t.Fatalf("window started at %s; the abort had to land before it", report.Phases.WindowStart)
	}
	if !report.Abort.At.Before(report.Phases.WorkEnd) {
		t.Fatalf("abort at %s is not before work end %s", report.Abort.At, report.Phases.WorkEnd)
	}
}

// TestAbortFileDuringSeedAborts: the abort file is watched from the start of
// the run, so a CPU or memory breach written during a long seed stops it.
func TestAbortFileDuringSeedAborts(t *testing.T) {
	leg := newFakeLeg(faults{}, false, 4)
	leg.enqueueDelay = 50 * time.Millisecond
	path := filepath.Join(t.TempDir(), "abort")
	opt := fastOptions()
	opt.AbortFile, opt.EnqueueBatch = path, 10
	go func() {
		time.Sleep(250 * time.Millisecond)
		_ = os.WriteFile(path, []byte("cpu over ceiling during seed"), 0o600)
	}()
	report, err := Run(context.Background(), fastSpec(), fakeLegFor(leg, nil, false), opt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustAbortBeforeWindow(t, report, TriggerExternal)
	if report.Phases.SeedSeconds != 0 {
		t.Fatalf("seed finished (%.2fs); the abort had to stop it", report.Phases.SeedSeconds)
	}
	assertCleanedUp(t, report, leg)
}

// TestHeadroomBreachDuringStatsWaitAborts: headroom is sampled through the
// post-seed statistics wait.
func TestHeadroomBreachDuringStatsWaitAborts(t *testing.T) {
	leg := newFakeLeg(faults{}, false, 4)
	leg.coldStats = true
	opt := fastOptions()
	opt.StatsWait = 3 * time.Second
	head := &fakeHeadroom{breach: leg.isSeeded}
	report, err := Run(context.Background(), fastSpec(), fakeLegFor(leg, head, false), opt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustAbortBeforeWindow(t, report, TriggerHeadroom)
	assertCleanedUp(t, report, leg)
}

// TestHeadroomBreachBeforeAnyWrite: a server already over its ceiling stops
// the run before the isolation probe writes anything.
func TestHeadroomBreachBeforeAnyWrite(t *testing.T) {
	leg := newFakeLeg(faults{}, false, 4)
	head := &fakeHeadroom{breach: func() bool { return true }}
	report, err := Run(context.Background(), fastSpec(), fakeLegFor(leg, head, false), fastOptions())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustAbortBeforeWindow(t, report, TriggerHeadroom)
	if report.Created != 0 || leg.isSeeded() {
		t.Fatalf("created %d items after a pre-run headroom breach", report.Created)
	}
}

// TestConsecutiveSamplerFailuresAbort: a sampler that cannot read the
// database fails closed instead of running blind.
func TestConsecutiveSamplerFailuresAbort(t *testing.T) {
	leg := newFakeLeg(faults{}, false, 4)
	leg.snapshotFailures = 1_000_000
	report, err := Run(context.Background(), fastSpec(), fakeLegFor(leg, nil, false), fastOptions())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Abort == nil || report.Abort.Trigger != TriggerSamplerUnavailable {
		t.Fatalf("abort = %+v, want %s", report.Abort, TriggerSamplerUnavailable)
	}
}

func TestConsecutiveHeadroomFailuresAbort(t *testing.T) {
	leg := newFakeLeg(faults{}, false, 4)
	head := &fakeHeadroom{fail: func() bool { return true }}
	report, err := Run(context.Background(), fastSpec(), fakeLegFor(leg, head, false), fastOptions())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Abort == nil || report.Abort.Trigger != TriggerSamplerUnavailable {
		t.Fatalf("abort = %+v, want %s", report.Abort, TriggerSamplerUnavailable)
	}
}

// TestSamplerErrorsAboveBudgetAreIncomplete: errors that never reach the
// consecutive limit still leave the run INCOMPLETE once they exceed the
// tolerated count.
func TestSamplerErrorsAboveBudgetAreIncomplete(t *testing.T) {
	leg := newFakeLeg(faults{}, false, 4)
	leg.snapshotFailures = 2
	opt := fastOptions()
	opt.MaxSampleErrors = 1
	report, err := Run(context.Background(), fastSpec(), fakeLegFor(leg, nil, false), opt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Abort != nil || report.Verdict != VerdictIncomplete {
		t.Fatalf("verdict %s abort %+v, want INCOMPLETE without abort", report.Verdict, report.Abort)
	}
}

func liveSpec() Spec {
	s := fastSpec()
	s.Target = TargetLive
	s.QuotaBudget = 0
	return s
}

func runLive(t *testing.T, head HeadroomSource, core func(context.Context) (int, error), opt Options) *Report {
	t.Helper()
	leg := newFakeLeg(faults{}, false, 0)
	stop := leg.startServerReaper(100*time.Millisecond, 20*time.Millisecond)
	defer stop()
	l := fakeLegFor(leg, head, true)
	l.Quota, l.Advisory = false, false
	opt.CoreResidue = core
	report, err := Run(context.Background(), liveSpec(), l, opt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return report
}

// TestLiveVerdictNeedsCoreResidueAndHeadroom: on the live target a missing
// third zero or a run with no headroom reading is INCOMPLETE, never CLEAN.
func TestLiveVerdictNeedsCoreResidueAndHeadroom(t *testing.T) {
	zero := func(context.Context) (int, error) { return 0, nil }
	if r := runLive(t, &fakeHeadroom{}, zero, fastOptions()); r.Verdict != VerdictClean {
		t.Fatalf("complete live run: verdict %s\n%s", r.Verdict, r.Summary())
	}
	if r := runLive(t, &fakeHeadroom{}, nil, fastOptions()); r.Verdict != VerdictIncomplete {
		t.Fatalf("live run without the core residue: verdict %s, want INCOMPLETE", r.Verdict)
	}
	if r := runLive(t, nil, zero, fastOptions()); r.Verdict != VerdictIncomplete {
		t.Fatalf("live run without headroom: verdict %s, want INCOMPLETE", r.Verdict)
	}
}

// TestLiveRefusesSamplerDisabled: the overhead-measurement mode never runs
// against the live target.
func TestLiveRefusesSamplerDisabled(t *testing.T) {
	leg := newFakeLeg(faults{}, false, 0)
	opt := fastOptions()
	opt.SamplerDisabled = true
	if _, err := Run(context.Background(), liveSpec(), fakeLegFor(leg, &fakeHeadroom{}, true), opt); err == nil {
		t.Fatalf("live run with the sampler disabled was accepted")
	}
	if leg.isSeeded() {
		t.Fatalf("refused run still wrote items")
	}
}

// TestReaperReturningNonSyntheticClaimAborts: the reap loop checks every
// reaped claim it is handed.
func TestReaperReturningNonSyntheticClaimAborts(t *testing.T) {
	report, leg := runFake(t, faults{reapRealAt: 2}, false, true, fastOptions())
	if report.Abort == nil || report.Abort.Trigger != TriggerNonSynthetic {
		t.Fatalf("abort = %+v, want %s", report.Abort, TriggerNonSynthetic)
	}
	assertCleanedUp(t, report, leg)
}

// TestParentCancelAbortsWithCleanup: SIGINT in the entry tests cancels the
// parent context; the run records an external abort and still reverses.
func TestParentCancelAbortsWithCleanup(t *testing.T) {
	leg := newFakeLeg(faults{}, false, 4)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(700 * time.Millisecond)
		cancel()
	}()
	report, err := Run(ctx, fastSpec(), fakeLegFor(leg, nil, false), fastOptions())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Abort == nil || report.Abort.Trigger != TriggerExternal {
		t.Fatalf("abort = %+v, want %s", report.Abort, TriggerExternal)
	}
	assertCleanedUp(t, report, leg)
}
