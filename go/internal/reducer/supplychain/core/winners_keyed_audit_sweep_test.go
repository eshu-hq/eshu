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
	delay  time.Duration
}

func (a *scriptedRangeAuditor) AuditWinners(context.Context) (SupplyChainImpactWinnersAudit, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.delay > 0 {
		time.Sleep(a.delay)
	}
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

// TestWinnersAuditTickStopsAtItsBudget pins the wall-time bound of one tick.
func TestWinnersAuditTickStopsAtItsBudget(t *testing.T) {
	t.Parallel()
	auditor := &scriptedRangeAuditor{passes: []SupplyChainImpactWinnersAudit{rangePass("advanced", true, true)}, delay: 20 * time.Millisecond}
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: &fakeWinnersRebuilder{}, LeaseManager: &fakeWinnersLeaseManager{claim: true}, Auditor: auditor,
		AuditTickBudget: 70 * time.Millisecond,
	}
	if err := m.runAuditOnce(context.Background()); err != nil {
		t.Fatalf("runAuditOnce() error = %v", err)
	}
	if auditor.calls < 2 || auditor.calls > 5 {
		t.Fatalf("auditor passes = %d in a 70 ms budget at 20 ms each, want 2 to 5", auditor.calls)
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
