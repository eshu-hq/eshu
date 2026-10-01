// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
	// releaseCtxErr and releaseCtxHasDeadline capture the state of the
	// context the runner handed to ReleasePartitionLease: a release that
	// runs on the cycle's own (possibly canceled) context strands the
	// lease for its full TTL (#6747 shape A).
	releaseCtxErr         error
	releaseCtxHasDeadline bool
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
	ctx context.Context,
	_ string,
	_, _ int,
	_ string,
) error {
	l.releaseCalls++
	l.releaseCtxErr = ctx.Err()
	_, l.releaseCtxHasDeadline = ctx.Deadline()
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

// TestGraphOrphanSweepRunnerEmitsLeaseTTLSeconds pins the #7047 P2: both the
// cycle-completed and the cycle-failed logs must carry the shared
// lease_ttl_seconds key, so deleting or renaming either emission regresses
// loudly instead of silently.
func TestGraphOrphanSweepRunnerEmitsLeaseTTLSeconds(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	successRunner := &GraphOrphanSweepRunner{
		Sweeper:      &fakeGraphOrphanSweeper{results: []GraphOrphanSweepResult{{Deleted: map[string]int64{}}}},
		LeaseManager: &fakeGraphOrphanLeaseManager{claimResults: []bool{true}},
		Config: GraphOrphanSweepRunnerConfig{
			LeaseOwner: "sweep-ttl-owner",
			LeaseTTL:   2 * time.Minute,
		},
		Logger: logger,
	}
	if _, err := successRunner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	failureRunner := &GraphOrphanSweepRunner{
		Sweeper:      &fakeGraphOrphanSweeper{errs: []error{errors.New("sweep boom")}},
		LeaseManager: &fakeGraphOrphanLeaseManager{claimResults: []bool{true}},
		Config: GraphOrphanSweepRunnerConfig{
			LeaseOwner: "sweep-ttl-owner",
			LeaseTTL:   2 * time.Minute,
		},
		Logger: logger,
		Wait: func(context.Context, time.Duration) error {
			cancel()
			return context.Canceled
		},
	}
	if err := failureRunner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	ttlByMessage := map[string]float64{}
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("unmarshal log line %q: %v", line, err)
		}
		msg, _ := record["msg"].(string)
		ttl, ok := record[telemetry.LogKeyLeaseTTLSeconds].(float64)
		if !ok {
			t.Fatalf("log %q missing %q: %v", msg, telemetry.LogKeyLeaseTTLSeconds, record)
		}
		ttlByMessage[msg] = ttl
	}
	for _, msg := range []string{"graph orphan sweep cycle completed", "graph orphan sweep cycle failed"} {
		got, ok := ttlByMessage[msg]
		if !ok {
			t.Fatalf("no log record with msg %q", msg)
		}
		if got != 120 {
			t.Fatalf("log %q %s = %v, want 120", msg, telemetry.LogKeyLeaseTTLSeconds, got)
		}
	}
}

// TestGraphOrphanSweepDefaultLeaseTTLCoversWriteBudget pins #7047: the
// effective lease TTL with no configured value must exceed the graph write
// budget plus a safety margin, so a write running to its full budget cannot
// reach the end of the lease with no margin. The 300s budget is ops-qa's
// ESHU_CANONICAL_WRITE_TIMEOUT; the 30s margin mirrors
// repoDependencyProjectionLeaseSafetyMargin.
func TestGraphOrphanSweepDefaultLeaseTTLCoversWriteBudget(t *testing.T) {
	const writeBudget = 300 * time.Second
	const safetyMargin = 30 * time.Second
	if got := (GraphOrphanSweepRunnerConfig{}).leaseTTL(); got <= writeBudget+safetyMargin {
		t.Fatalf("default lease TTL = %v, want more than %v", got, writeBudget+safetyMargin)
	}
}

// TestGraphOrphanSweepRunnerReleasesLeaseAfterCancel pins the #7047 P2:
// when shutdown cancels the cycle context mid-sweep, the deferred lease
// release must still run on a live, bounded context. Releasing through the
// canceled cycle context hands Postgres an already-dead request, the release
// fails, and the row stays held for the full TTL under an owner that no
// longer exists (#6747 shape A).
func TestGraphOrphanSweepRunnerReleasesLeaseAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	leaseManager := &fakeGraphOrphanLeaseManager{claimResults: []bool{true}}
	runner := &GraphOrphanSweepRunner{
		Sweeper:      &cancelingGraphOrphanSweeper{cancel: cancel},
		LeaseManager: leaseManager,
		Config: GraphOrphanSweepRunnerConfig{
			LeaseOwner: "sweep-owner-cancel",
			LeaseTTL:   time.Minute,
		},
	}
	if _, err := runner.RunOnce(ctx); err == nil {
		t.Fatal("RunOnce() error = nil, want cancellation error")
	}
	if leaseManager.releaseCalls != 1 {
		t.Fatalf("release calls = %d, want 1", leaseManager.releaseCalls)
	}
	if err := leaseManager.releaseCtxErr; err != nil {
		t.Fatalf("release context error = %v, want live release context", err)
	}
	if !leaseManager.releaseCtxHasDeadline {
		t.Fatal("release context has no deadline, want bounded release")
	}
}

// cancelingGraphOrphanSweeper simulates shutdown landing mid-sweep: it
// cancels the cycle context, then reports the cancellation as the cycle
// error so RunOnce unwinds through its deferred lease release.
type cancelingGraphOrphanSweeper struct {
	cancel context.CancelFunc
}

func (s *cancelingGraphOrphanSweeper) SweepOrphanNodes(
	ctx context.Context,
	_ GraphOrphanSweepPolicy,
) (GraphOrphanSweepResult, error) {
	s.cancel()
	return GraphOrphanSweepResult{}, ctx.Err()
}
