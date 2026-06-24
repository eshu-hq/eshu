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

func TestGraphOrphanSweepRunnerDrainsUntilNoDeletedNodes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sweeper := &fakeGraphOrphanSweeper{
		results: []GraphOrphanSweepResult{
			{Deleted: map[string]int64{"Repository": 2}},
			{Deleted: map[string]int64{}},
		},
	}
	waitCalls := 0
	runner := &GraphOrphanSweepRunner{
		Sweeper: sweeper,
		Config: GraphOrphanSweepRunnerConfig{
			PollInterval: time.Hour,
			Policy: GraphOrphanSweepPolicy{
				OrphanTTL:  7 * 24 * time.Hour,
				BatchLimit: 100,
				CountLimit: 1000,
				Labels:     []string{"Repository", "Platform"},
			},
		},
		Wait: func(context.Context, time.Duration) error {
			waitCalls++
			cancel()
			return context.Canceled
		},
	}

	err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := sweeper.callCount(); got != 2 {
		t.Fatalf("sweeper calls = %d, want 2", got)
	}
	if waitCalls != 1 {
		t.Fatalf("wait calls = %d, want 1", waitCalls)
	}
	if got := sweeper.policies[0].OrphanTTL; got != 7*24*time.Hour {
		t.Fatalf("policy ttl = %v, want 168h", got)
	}
}

func TestGraphOrphanSweepRunnerSkipsWhenLeaseUnavailable(t *testing.T) {
	sweeper := &fakeGraphOrphanSweeper{
		results: []GraphOrphanSweepResult{{Deleted: map[string]int64{"Repository": 1}}},
	}
	leaseManager := &fakeGraphOrphanLeaseManager{claimResults: []bool{false}}
	runner := &GraphOrphanSweepRunner{
		Sweeper:      sweeper,
		LeaseManager: leaseManager,
		Config: GraphOrphanSweepRunnerConfig{
			LeaseOwner: "sweep-owner-1",
			LeaseTTL:   time.Minute,
		},
	}

	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}
	if result.LeaseAcquired {
		t.Fatal("LeaseAcquired = true, want false")
	}
	if got := sweeper.callCount(); got != 0 {
		t.Fatalf("sweeper calls = %d, want 0 when lease is unavailable", got)
	}
	if leaseManager.releaseCalls != 0 {
		t.Fatalf("release calls = %d, want 0 without a claimed lease", leaseManager.releaseCalls)
	}
}

func TestGraphOrphanSweepRunnerClaimsAndReleasesLease(t *testing.T) {
	sweeper := &fakeGraphOrphanSweeper{
		results: []GraphOrphanSweepResult{{Deleted: map[string]int64{}}},
	}
	leaseManager := &fakeGraphOrphanLeaseManager{claimResults: []bool{true}}
	runner := &GraphOrphanSweepRunner{
		Sweeper:      sweeper,
		LeaseManager: leaseManager,
		Config: GraphOrphanSweepRunnerConfig{
			LeaseOwner: "sweep-owner-2",
			LeaseTTL:   2 * time.Minute,
		},
	}

	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}
	if !result.LeaseAcquired {
		t.Fatal("LeaseAcquired = false, want true")
	}
	if got := sweeper.callCount(); got != 1 {
		t.Fatalf("sweeper calls = %d, want 1", got)
	}
	if got := leaseManager.claimOwner; got != "sweep-owner-2" {
		t.Fatalf("claim owner = %q, want configured owner", got)
	}
	if got := leaseManager.claimTTL; got != 2*time.Minute {
		t.Fatalf("claim TTL = %v, want 2m", got)
	}
	if leaseManager.releaseCalls != 1 {
		t.Fatalf("release calls = %d, want 1", leaseManager.releaseCalls)
	}
}

func TestGraphOrphanSweepRunnerValidation(t *testing.T) {
	runner := &GraphOrphanSweepRunner{}

	_, err := runner.RunOnce(context.Background())

	if err == nil || !errors.Is(err, ErrGraphOrphanSweeperRequired) {
		t.Fatalf("RunOnce() error = %v, want ErrGraphOrphanSweeperRequired", err)
	}
}

func TestServiceStartsGraphOrphanSweepRunner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sweeper := &fakeGraphOrphanSweeper{
		results: []GraphOrphanSweepResult{{Deleted: map[string]int64{}}},
	}
	started := make(chan struct{}, 1)
	runner := &GraphOrphanSweepRunner{
		Sweeper: sweeper,
		Config:  GraphOrphanSweepRunnerConfig{PollInterval: time.Hour},
		Wait: func(ctx context.Context, _ time.Duration) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		},
	}
	service := Service{GraphOrphanSweepRunner: runner}
	var wg sync.WaitGroup
	var gotErr error
	service.startSideRunners(ctx, &wg, func(err error) {
		if !errors.Is(err, context.Canceled) {
			gotErr = err
		}
	})

	deadline := time.After(time.Second)
	for sweeper.callCount() != 1 {
		select {
		case <-deadline:
			t.Fatal("sweeper was not called")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	<-started
	cancel()
	wg.Wait()

	if gotErr != nil {
		t.Fatalf("side runner error = %v, want nil", gotErr)
	}
}

type fakeGraphOrphanSweeper struct {
	mu       sync.Mutex
	calls    int
	policies []GraphOrphanSweepPolicy
	results  []GraphOrphanSweepResult
	errs     []error
}

type fakeGraphOrphanLeaseManager struct {
	claimResults []bool
	claimCalls   int
	releaseCalls int
	claimOwner   string
	claimTTL     time.Duration
}

func (l *fakeGraphOrphanLeaseManager) ClaimPartitionLease(
	_ context.Context,
	_ string,
	_, _ int,
	owner string,
	ttl time.Duration,
) (bool, error) {
	l.claimCalls++
	l.claimOwner = owner
	l.claimTTL = ttl
	if len(l.claimResults) == 0 {
		return true, nil
	}
	result := l.claimResults[0]
	l.claimResults = l.claimResults[1:]
	return result, nil
}

func (l *fakeGraphOrphanLeaseManager) ReleasePartitionLease(
	_ context.Context,
	_ string,
	_, _ int,
	_ string,
) error {
	l.releaseCalls++
	return nil
}

func (s *fakeGraphOrphanSweeper) SweepOrphanNodes(
	_ context.Context,
	policy GraphOrphanSweepPolicy,
) (GraphOrphanSweepResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.policies = append(s.policies, policy)
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		if err != nil {
			return GraphOrphanSweepResult{}, err
		}
	}
	if len(s.results) == 0 {
		return GraphOrphanSweepResult{Deleted: map[string]int64{}}, nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}

func (s *fakeGraphOrphanSweeper) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}
