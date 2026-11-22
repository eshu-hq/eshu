// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// scriptedRangeAuditor returns its scripted passes in order, then repeats the
// last one.
type scriptedRangeAuditor struct {
	mu     sync.Mutex
	passes []SupplyChainImpactWinnersAudit
	calls  int
}

func (a *scriptedRangeAuditor) AuditWinners(context.Context) (SupplyChainImpactWinnersAudit, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	i := min(a.calls, len(a.passes)-1)
	a.calls++
	return a.passes[i], nil
}

func rangePass(outcome string, more, partial bool) SupplyChainImpactWinnersAudit {
	return SupplyChainImpactWinnersAudit{Outcome: outcome, More: more, Partial: partial}
}

// TestWinnersAuditTickRunsSweepRangesBackToBack pins the #11131 tick loop: a
// range-sweep auditor runs range after range in one audit tick while it
// reports More, renewing the audit lease before each further range, and only
// the range that completes the sweep records the audit commit (the
// last-success age keeps meaning a completed pass).
func TestWinnersAuditTickRunsSweepRangesBackToBack(t *testing.T) {
	t.Parallel()
	inst, reader := winnersSignalsHarness(t)
	collect := func(rm *metricdata.ResourceMetrics) error { return reader.Collect(context.Background(), rm) }
	const ageGauge = "eshu_dp_supply_chain_impact_winners_audit_last_success_age_seconds"

	auditor := &scriptedRangeAuditor{passes: []SupplyChainImpactWinnersAudit{
		rangePass("advanced", true, true),
		rangePass("skipped", true, true),
		rangePass("advanced", true, true),
	}}
	lease := &fakeWinnersLeaseManager{claim: true}
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: &fakeWinnersRebuilder{}, LeaseManager: lease, Auditor: auditor, Instruments: inst,
		AuditTickBudget: time.Hour,
	}
	// Stop the endless script after six passes through the lease: the
	// seventh renewal is refused, which must end the tick.
	stopAfter := &leaseStopAfter{inner: lease, allow: 6}
	m.LeaseManager = stopAfter
	if err := m.runAuditOnce(context.Background()); err != nil {
		t.Fatalf("runAuditOnce() error = %v", err)
	}
	if auditor.calls != 6 {
		t.Fatalf("auditor passes = %d, want 6 (one claim plus five renewals, then the lost renewal ends the tick)", auditor.calls)
	}
	if got := winnersGaugeValues(t, collect, ageGauge); len(got) != 0 {
		t.Fatalf("audit age after partial ranges only = %v, want no value: a range is not a completed pass", got)
	}

	done := &scriptedRangeAuditor{passes: []SupplyChainImpactWinnersAudit{
		rangePass("advanced", true, true),
		rangePass("completed", false, false),
	}}
	m.Auditor, m.LeaseManager = done, &fakeWinnersLeaseManager{claim: true}
	if err := m.runAuditOnce(context.Background()); err != nil {
		t.Fatalf("runAuditOnce() error = %v", err)
	}
	if done.calls != 2 {
		t.Fatalf("auditor passes = %d, want 2 (the completing range reports no More)", done.calls)
	}
	if got := winnersGaugeValues(t, collect, ageGauge); len(got) != 1 {
		t.Fatalf("audit age after the completing range = %v, want one sample", got)
	}
}

// fakeTickClock is a settable clock for the audit tick budget. The auditor
// advances it, so how many passes fit in a budget is decided by the script, not
// by how fast the scheduler runs the test.
type fakeTickClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeTickClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeTickClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// clockAdvancingAuditor reports an unfinished range every pass and spends
// perPass of fake time on it.
type clockAdvancingAuditor struct {
	clock   *fakeTickClock
	perPass time.Duration
	calls   int
}

func (a *clockAdvancingAuditor) AuditWinners(context.Context) (SupplyChainImpactWinnersAudit, error) {
	a.calls++
	a.clock.advance(a.perPass)
	return rangePass("advanced", true, true), nil
}

// TestWinnersAuditTickStopsAtItsBudget pins the time bound of one audit tick
// (#11131) on an injected clock: the tick runs the first pass, keeps running
// ranges while the budget has time left, and stops on the first pass that
// ends at or past the deadline. The pass count is exact because the auditor,
// not the scheduler, advances the clock (#11247).
func TestWinnersAuditTickStopsAtItsBudget(t *testing.T) {
	t.Parallel()
	const perPass = 20 * time.Millisecond
	tests := []struct {
		name       string
		budget     time.Duration
		wantPasses int
	}{
		{name: "stops after the pass that crosses the deadline", budget: 70 * time.Millisecond, wantPasses: 4},
		{name: "stops when a pass ends exactly on the deadline", budget: 60 * time.Millisecond, wantPasses: 3},
		{name: "a budget shorter than one pass still runs that pass", budget: time.Millisecond, wantPasses: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			clock := &fakeTickClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
			auditor := &clockAdvancingAuditor{clock: clock, perPass: perPass}
			// The lease cap ends a tick that ignores its budget, so a broken
			// budget fails this assertion instead of hanging the test.
			lease := &leaseStopAfter{inner: &fakeWinnersLeaseManager{claim: true}, allow: 50}
			m := SupplyChainImpactWinnersMaintainer{
				Rebuilder: &fakeWinnersRebuilder{}, LeaseManager: lease, Auditor: auditor,
				AuditTickBudget: tt.budget, Now: clock.Now,
			}
			if err := m.runAuditOnce(context.Background()); err != nil {
				t.Fatalf("runAuditOnce() error = %v", err)
			}
			if auditor.calls != tt.wantPasses {
				t.Fatalf("auditor passes = %d in a %s budget at %s each, want exactly %d",
					auditor.calls, tt.budget, perPass, tt.wantPasses)
			}
		})
	}
}

// TestWinnersAuditTickRunsOneWholeTablePass keeps a whole-table auditor at
// one pass per tick.
func TestWinnersAuditTickRunsOneWholeTablePass(t *testing.T) {
	t.Parallel()
	auditor := &fakeWinnersAuditor{}
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: &fakeWinnersRebuilder{}, LeaseManager: &fakeWinnersLeaseManager{claim: true}, Auditor: auditor,
	}
	if err := m.runAuditOnce(context.Background()); err != nil {
		t.Fatalf("runAuditOnce() error = %v", err)
	}
	if auditor.count() != 1 {
		t.Fatalf("whole-table passes = %d, want 1", auditor.count())
	}
}

// leaseStopAfter grants the first allow claims and refuses every later one.
type leaseStopAfter struct {
	inner *fakeWinnersLeaseManager
	allow int
	seen  int
}

func (l *leaseStopAfter) ClaimPartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, owner string, ttl time.Duration) (bool, error) {
	l.seen++
	if l.seen > l.allow {
		return false, nil
	}
	return l.inner.ClaimPartitionLease(ctx, domain, partitionID, partitionCount, owner, ttl)
}

func (l *leaseStopAfter) ReleasePartitionLease(ctx context.Context, domain string, partitionID, partitionCount int, owner string) error {
	return l.inner.ReleasePartitionLease(ctx, domain, partitionID, partitionCount, owner)
}
