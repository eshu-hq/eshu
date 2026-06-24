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

type fakeWinnersRebuilder struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeWinnersRebuilder) RebuildAllWinners(_ context.Context, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

func (f *fakeWinnersRebuilder) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeWinnersLeaseManager struct {
	mu        sync.Mutex
	claim     bool
	claimErr  error
	claims    int
	releases  int
	heldAfter bool // tracks that release follows every successful claim
}

func (f *fakeWinnersLeaseManager) ClaimPartitionLease(_ context.Context, domain string, partitionID, partitionCount int, _ string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if domain != SupplyChainImpactWinnersDomain || partitionID != 0 || partitionCount != 1 {
		return false, errors.New("unexpected lease scope")
	}
	if f.claimErr != nil {
		return false, f.claimErr
	}
	if f.claim {
		f.claims++
		f.heldAfter = true
	}
	return f.claim, nil
}

func (f *fakeWinnersLeaseManager) ReleasePartitionLease(_ context.Context, _ string, _, _ int, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	f.heldAfter = false
	return nil
}

func TestWinnersMaintainerRunOnceRebuildsWhenLeaseAcquired(t *testing.T) {
	t.Parallel()

	rebuilder := &fakeWinnersRebuilder{}
	lease := &fakeWinnersLeaseManager{claim: true}
	m := SupplyChainImpactWinnersMaintainer{Rebuilder: rebuilder, LeaseManager: lease}

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

func TestWinnersMaintainerRunOnceSkipsWhenLeaseNotAcquired(t *testing.T) {
	t.Parallel()

	rebuilder := &fakeWinnersRebuilder{}
	lease := &fakeWinnersLeaseManager{claim: false}
	m := SupplyChainImpactWinnersMaintainer{Rebuilder: rebuilder, LeaseManager: lease}

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

func TestWinnersMaintainerRunOnceReleasesLeaseOnRebuildError(t *testing.T) {
	t.Parallel()

	rebuilder := &fakeWinnersRebuilder{err: errors.New("boom")}
	lease := &fakeWinnersLeaseManager{claim: true}
	m := SupplyChainImpactWinnersMaintainer{Rebuilder: rebuilder, LeaseManager: lease}

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
	if lease.heldAfter {
		t.Fatal("lease still held after error, want released")
	}
}

// TestWinnersMaintainerRunOnceIsIdempotent covers the replay matrix: re-running a
// converged resweep is a safe no-op-equivalent (the atomic rebuild reconciles to
// the same state), so duplicate cycles never corrupt the winners table.
func TestWinnersMaintainerRunOnceIsIdempotent(t *testing.T) {
	t.Parallel()

	rebuilder := &fakeWinnersRebuilder{}
	lease := &fakeWinnersLeaseManager{claim: true}
	m := SupplyChainImpactWinnersMaintainer{Rebuilder: rebuilder, LeaseManager: lease}

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

func TestWinnersMaintainerValidate(t *testing.T) {
	t.Parallel()

	if _, err := (SupplyChainImpactWinnersMaintainer{LeaseManager: &fakeWinnersLeaseManager{}}).RunOnce(context.Background()); err == nil {
		t.Fatal("missing rebuilder must error")
	}
	if _, err := (SupplyChainImpactWinnersMaintainer{Rebuilder: &fakeWinnersRebuilder{}}).RunOnce(context.Background()); err == nil {
		t.Fatal("missing lease manager must error")
	}
}

func TestWinnersMaintainerRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	rebuilder := &fakeWinnersRebuilder{}
	lease := &fakeWinnersLeaseManager{claim: true}
	m := SupplyChainImpactWinnersMaintainer{
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
