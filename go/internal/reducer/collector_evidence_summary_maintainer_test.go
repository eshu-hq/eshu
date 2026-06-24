// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeCollectorEvidenceRebuilder struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeCollectorEvidenceRebuilder) RebuildAllCollectorEvidence(_ context.Context, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

func (f *fakeCollectorEvidenceRebuilder) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeCollectorEvidenceLeaseManager struct {
	mu       sync.Mutex
	claim    bool
	claimErr error
	claims   int
	releases int
	held     bool
}

func (f *fakeCollectorEvidenceLeaseManager) ClaimPartitionLease(_ context.Context, domain string, partitionID, partitionCount int, _ string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if domain != CollectorEvidenceSummaryDomain || partitionID != 0 || partitionCount != 1 {
		return false, errors.New("unexpected lease scope")
	}
	if f.claimErr != nil {
		return false, f.claimErr
	}
	if f.claim {
		f.claims++
		f.held = true
	}
	return f.claim, nil
}

func (f *fakeCollectorEvidenceLeaseManager) ReleasePartitionLease(_ context.Context, _ string, _, _ int, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	f.held = false
	return nil
}

func TestCollectorEvidenceMaintainerRunOnceRebuildsWhenLeaseAcquired(t *testing.T) {
	t.Parallel()

	rebuilder := &fakeCollectorEvidenceRebuilder{}
	lease := &fakeCollectorEvidenceLeaseManager{claim: true}
	m := CollectorEvidenceSummaryMaintainer{Rebuilder: rebuilder, LeaseManager: lease}

	rebuilt, err := m.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error = %v, want nil", err)
	}
	if !rebuilt {
		t.Fatal("rebuilt = false, want true when lease acquired")
	}
	if rebuilder.count() != 1 {
		t.Fatalf("rebuild calls = %d, want 1", rebuilder.count())
	}
	if lease.releases != 1 {
		t.Fatalf("lease releases = %d, want 1 (lease must always be released)", lease.releases)
	}
}

func TestCollectorEvidenceMaintainerRunOnceSkipsWhenLeaseNotAcquired(t *testing.T) {
	t.Parallel()

	rebuilder := &fakeCollectorEvidenceRebuilder{}
	lease := &fakeCollectorEvidenceLeaseManager{claim: false}
	m := CollectorEvidenceSummaryMaintainer{Rebuilder: rebuilder, LeaseManager: lease}

	rebuilt, err := m.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error = %v, want nil", err)
	}
	if rebuilt {
		t.Fatal("rebuilt = true, want false when another owner holds the lease")
	}
	if rebuilder.count() != 0 {
		t.Fatalf("rebuild calls = %d, want 0 when lease not acquired", rebuilder.count())
	}
}

func TestCollectorEvidenceMaintainerRunOnceReleasesLeaseOnRebuildError(t *testing.T) {
	t.Parallel()

	rebuilder := &fakeCollectorEvidenceRebuilder{err: errors.New("boom")}
	lease := &fakeCollectorEvidenceLeaseManager{claim: true}
	m := CollectorEvidenceSummaryMaintainer{Rebuilder: rebuilder, LeaseManager: lease}

	rebuilt, err := m.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce error = nil, want rebuild error")
	}
	if rebuilt {
		t.Fatal("rebuilt = true, want false on rebuild error")
	}
	if lease.releases != 1 {
		t.Fatalf("lease releases = %d, want 1 (lease released even on error)", lease.releases)
	}
	if lease.held {
		t.Fatal("lease still held after error, want released")
	}
}

// TestCollectorEvidenceMaintainerRunOnceIsIdempotent covers the replay matrix:
// re-running a converged resweep is a safe no-op-equivalent (the atomic rebuild
// reconciles to the same state), so duplicate cycles never corrupt the summary.
func TestCollectorEvidenceMaintainerRunOnceIsIdempotent(t *testing.T) {
	t.Parallel()

	rebuilder := &fakeCollectorEvidenceRebuilder{}
	lease := &fakeCollectorEvidenceLeaseManager{claim: true}
	m := CollectorEvidenceSummaryMaintainer{Rebuilder: rebuilder, LeaseManager: lease}

	for i := 0; i < 3; i++ {
		if _, err := m.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce[%d] error = %v", i, err)
		}
	}
	if rebuilder.count() != 3 {
		t.Fatalf("rebuild calls = %d, want 3", rebuilder.count())
	}
	if lease.claims != 3 || lease.releases != 3 {
		t.Fatalf("claims=%d releases=%d, want 3/3 (claim+release balanced per cycle)", lease.claims, lease.releases)
	}
}

type fakeCollectorEvidenceFreshness struct {
	last  time.Time
	ok    bool
	err   error
	calls int
}

func (f *fakeCollectorEvidenceFreshness) LastCollectorEvidenceMaterializedAt(_ context.Context) (time.Time, bool, error) {
	f.calls++
	return f.last, f.ok, f.err
}

// TestCollectorEvidenceMaintainerRunOnceSkipsWhenSummaryFresh covers the #3471
// review fix: with multiple replicas the lease is released after each resweep, so
// every replica could otherwise reclaim it and run the full O(active facts) scan
// on its own cadence. The durable last-materialized guard makes a replica skip the
// resweep when the summary is newer than the cadence, capping cluster-wide
// resweeps at ~one per cadence regardless of replica count.
func TestCollectorEvidenceMaintainerRunOnceSkipsWhenSummaryFresh(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 1, 0, 0, 0, time.UTC)
	rebuilder := &fakeCollectorEvidenceRebuilder{}
	lease := &fakeCollectorEvidenceLeaseManager{claim: true}
	fresh := &fakeCollectorEvidenceFreshness{last: now.Add(-10 * time.Second), ok: true}
	m := CollectorEvidenceSummaryMaintainer{
		Rebuilder:    rebuilder,
		LeaseManager: lease,
		Freshness:    fresh,
		Now:          func() time.Time { return now },
		Interval:     60 * time.Second,
	}

	rebuilt, err := m.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error = %v, want nil", err)
	}
	if rebuilt {
		t.Fatal("rebuilt = true, want false when summary is fresher than the cadence")
	}
	if rebuilder.count() != 0 {
		t.Fatalf("rebuild calls = %d, want 0 when summary is fresh (no redundant fact scan)", rebuilder.count())
	}
	if lease.releases != 1 {
		t.Fatalf("lease releases = %d, want 1 (lease claimed and released even when skipping)", lease.releases)
	}
}

func TestCollectorEvidenceMaintainerRunOnceResweepsWhenSummaryStale(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 1, 0, 0, 0, time.UTC)
	rebuilder := &fakeCollectorEvidenceRebuilder{}
	lease := &fakeCollectorEvidenceLeaseManager{claim: true}
	stale := &fakeCollectorEvidenceFreshness{last: now.Add(-90 * time.Second), ok: true}
	m := CollectorEvidenceSummaryMaintainer{
		Rebuilder:    rebuilder,
		LeaseManager: lease,
		Freshness:    stale,
		Now:          func() time.Time { return now },
		Interval:     60 * time.Second,
	}

	rebuilt, err := m.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error = %v, want nil", err)
	}
	if !rebuilt || rebuilder.count() != 1 {
		t.Fatalf("rebuilt=%v calls=%d, want true/1 when summary is older than the cadence", rebuilt, rebuilder.count())
	}
}

func TestCollectorEvidenceMaintainerRunOnceResweepsWhenNeverMaterialized(t *testing.T) {
	t.Parallel()

	rebuilder := &fakeCollectorEvidenceRebuilder{}
	lease := &fakeCollectorEvidenceLeaseManager{claim: true}
	empty := &fakeCollectorEvidenceFreshness{ok: false} // no summary rows yet (startup backfill)
	m := CollectorEvidenceSummaryMaintainer{
		Rebuilder:    rebuilder,
		LeaseManager: lease,
		Freshness:    empty,
	}

	rebuilt, err := m.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce error = %v, want nil", err)
	}
	if !rebuilt || rebuilder.count() != 1 {
		t.Fatalf("rebuilt=%v calls=%d, want true/1 when summary has never been materialized", rebuilt, rebuilder.count())
	}
}

func TestCollectorEvidenceMaintainerValidate(t *testing.T) {
	t.Parallel()

	if _, err := (CollectorEvidenceSummaryMaintainer{LeaseManager: &fakeCollectorEvidenceLeaseManager{}}).RunOnce(context.Background()); err == nil {
		t.Fatal("missing rebuilder must error")
	}
	if _, err := (CollectorEvidenceSummaryMaintainer{Rebuilder: &fakeCollectorEvidenceRebuilder{}}).RunOnce(context.Background()); err == nil {
		t.Fatal("missing lease manager must error")
	}
}

// TestCollectorEvidenceMaintainerStaleWindowMargin pins the #3466 staleness-verdict
// safety argument: the resweep cadence MUST be far smaller than the collector
// promotion stale window so a one-cadence materialization lag on the summary's
// MAX timestamps can never flip a CollectorPromotionStale verdict.
func TestCollectorEvidenceMaintainerStaleWindowMargin(t *testing.T) {
	t.Parallel()

	cadence := CollectorEvidenceSummaryMaintainer{}.interval()
	if cadence <= 0 {
		t.Fatalf("default cadence = %v, want > 0", cadence)
	}
	const collectorPromotionStaleWindow = 24 * time.Hour // status.DefaultCollectorPromotionStaleAfter
	if cadence*100 >= collectorPromotionStaleWindow {
		t.Fatalf("cadence %v too close to stale window %v; lag could flip a stale verdict", cadence, collectorPromotionStaleWindow)
	}
}

func TestCollectorEvidenceMaintainerRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	rebuilder := &fakeCollectorEvidenceRebuilder{}
	lease := &fakeCollectorEvidenceLeaseManager{claim: true}
	m := CollectorEvidenceSummaryMaintainer{
		Rebuilder:    rebuilder,
		LeaseManager: lease,
		Interval:     time.Hour, // long cadence; cancel must break the wait
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil on cancel", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
