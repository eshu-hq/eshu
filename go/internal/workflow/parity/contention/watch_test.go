// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestLiveExporterWithoutPgSeriesIsNotClean: an exporter URL that answers a
// valid Prometheus page with no pg_ families (the wrong port-forward) is a
// failed headroom read on the live target, never a clean run.
func TestLiveExporterWithoutPgSeriesIsNotClean(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("# TYPE go_goroutines gauge\ngo_goroutines 12\nprocess_cpu_seconds_total 3.5\n"))
	}))
	defer page.Close()
	spec := liveSpec()
	spec.Headroom.MaxLagSeconds = 30
	report := runLiveSpec(t, spec, ExporterHeadroom{URL: page.URL}, func(context.Context) (int, error) { return 0, nil })
	if report.Verdict == VerdictClean {
		t.Fatalf("live run with an exporter page carrying no pg_ series: verdict CLEAN\n%s", report.Summary())
	}
	if report.Abort == nil || report.Abort.Trigger != TriggerSamplerUnavailable {
		t.Fatalf("abort = %+v, want %s from the failed headroom reads", report.Abort, TriggerSamplerUnavailable)
	}
}

// TestLiveCeilingNeverMeasuredIsIncomplete: a live headroom source that
// never reports an enforced ceiling leaves the run INCOMPLETE even when no
// single read failed (here the lag ceiling: the page has no lag series).
func TestLiveCeilingNeverMeasuredIsIncomplete(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pg_stat_database_numbackends{datname=\"eshu\"} 10\n" +
			"pg_stat_activity_count{datname=\"eshu\",wait_event_type=\"Lock\"} 0\n" +
			"pg_stat_database_deadlocks{datname=\"eshu\"} 1\n"))
	}))
	defer page.Close()
	spec := liveSpec()
	report := runLiveSpec(t, spec, ExporterHeadroom{URL: page.URL}, func(context.Context) (int, error) { return 0, nil })
	if report.Verdict != VerdictClean {
		t.Fatalf("complete exporter page without a lag ceiling: verdict %s (%s)", report.Verdict, report.Incomplete)
	}
	spec.Headroom.MaxLagSeconds = 30
	report = runLiveSpec(t, spec, ExporterHeadroom{URL: page.URL}, func(context.Context) (int, error) { return 0, nil })
	if report.Verdict == VerdictClean {
		t.Fatalf("lag ceiling set but never measured: verdict CLEAN")
	}
}

func runLiveSpec(t *testing.T, spec Spec, head HeadroomSource, core func(context.Context) (int, error)) *Report {
	t.Helper()
	leg := newFakeLeg(faults{}, false, 0)
	stop := leg.startServerReaper(100*time.Millisecond, 20*time.Millisecond)
	defer stop()
	l := fakeLegFor(leg, head, true)
	l.Quota, l.Advisory = false, false
	opt := fastOptions()
	opt.CoreResidue = core
	report, err := Run(context.Background(), spec, l, opt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return report
}

// TestFaultDuringSeedAbortsBeforeWindow: the database sampler watches the
// seed itself, so a row lost while the backlog is written stops the run
// before the window opens.
func TestFaultDuringSeedAbortsBeforeWindow(t *testing.T) {
	leg := newFakeLeg(faults{loseInSeedAt: 3}, false, 4)
	leg.enqueueDelay = 40 * time.Millisecond
	opt := fastOptions()
	opt.EnqueueBatch = 10
	report, err := Run(context.Background(), fastSpec(), fakeLegFor(leg, nil, false), opt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustAbortBeforeWindow(t, report, TriggerLostItem)
	if report.Abort.Source != SourceSampler {
		t.Fatalf("abort source %q, want the sampler", report.Abort.Source)
	}
}

// TestFaultDuringStatsWaitAbortsBeforeWindow: the sampler also covers the
// post-seed statistics wait.
func TestFaultDuringStatsWaitAbortsBeforeWindow(t *testing.T) {
	leg := newFakeLeg(faults{loseInStats: true}, false, 4)
	leg.coldStats = true
	opt := fastOptions()
	opt.StatsWait = 2 * time.Second
	report, err := Run(context.Background(), fastSpec(), fakeLegFor(leg, nil, false), opt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mustAbortBeforeWindow(t, report, TriggerLostItem)
	if report.Abort.Source != SourceSampler {
		t.Fatalf("abort source %q, want the sampler", report.Abort.Source)
	}
}

// TestNoSamplerReadingIsIncomplete: a run whose database sampler never
// ticked has no invariant evidence and cannot be CLEAN.
func TestNoSamplerReadingIsIncomplete(t *testing.T) {
	leg := newFakeLeg(faults{}, false, 4)
	opt := fastOptions()
	opt.SampleInterval = 10 * time.Minute
	report, err := Run(context.Background(), fastSpec(), fakeLegFor(leg, nil, false), opt)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Verdict != VerdictIncomplete || report.Incomplete == "" {
		t.Fatalf("verdict %s (%q), want INCOMPLETE for a run with no sampler reading", report.Verdict, report.Incomplete)
	}
}

// TestAbortBeforeBaselineDeletesNothing: a run stopped before its baseline
// census never learned which rows were its own, so the reversal deletes
// nothing, not even rows in this seed's own id family.
func TestAbortBeforeBaselineDeletesNothing(t *testing.T) {
	leg := newFakeLeg(faults{}, false, 4)
	leftover := Prefix + "wi-7-earlier-run"
	leg.items[leftover] = &fakeItem{item: workflowItemID(leftover)}
	head := &fakeHeadroom{breach: func() bool { return true }}
	report, err := Run(context.Background(), fastSpec(), fakeLegFor(leg, head, false), fastOptions())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := leg.items[leftover]; !ok || len(report.Deleted) != 0 || report.CleanupSkipped == "" {
		t.Fatalf("abort before the baseline deleted rows: kept=%v deleted=%v skipped=%q", ok, report.Deleted, report.CleanupSkipped)
	}
}

// TestIncompleteNamesUnmeasuredCeiling: the verdict rule itself, apart from
// the watcher that normally fails such reads first.
func TestIncompleteNamesUnmeasuredCeiling(t *testing.T) {
	r := &runner{spec: liveSpec(), opt: Options{MaxSampleErrors: 5}}
	r.spec.Headroom.MaxLagSeconds = 30
	r.samples.count = 10
	r.samples.addHead(HeadroomSample{Connections: 5, LockWaiters: 0, DeadlocksTotal: 0, ReplicationLagSec: notAvailable})
	report := &Report{Residue: Residue{ThirdZero: CoreResidue{Checked: true}}}
	if reason := r.incomplete(report); reason == "" {
		t.Fatalf("a live run that never measured the lag ceiling was not INCOMPLETE")
	}
	r.spec.Headroom.MaxLagSeconds = 0
	if reason := r.incomplete(report); reason != "" {
		t.Fatalf("no lag ceiling set: incomplete = %q, want complete", reason)
	}
}
