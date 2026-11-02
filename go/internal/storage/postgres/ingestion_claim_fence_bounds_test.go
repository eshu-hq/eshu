// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/claim/fence"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestPlaneCommitRefusesMarginAtOrAboveTwoThirdsOfLease enforces the D6 MUST
// margin < lease - lease/3 before any pin or transaction: with a short lease
// every commit would otherwise fail the deadline check retryably forever.
func TestPlaneCommitRefusesMarginAtOrAboveTwoThirdsOfLease(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		lease  time.Duration
		margin time.Duration
		refuse bool
	}{
		{"default margin, 7.5s lease", 7500 * time.Millisecond, 0, true},
		{"default margin, 6s lease", 6 * time.Second, 0, true},
		{"default margin, 7.6s lease", 7600 * time.Millisecond, 0, false},
		{"default margin, default lease", time.Minute, 0, false},
		{"40s margin, 60s lease", time.Minute, 40 * time.Second, true},
		{"39s margin, 60s lease", time.Minute, 39 * time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pinner := &claimFenceFakePinner{}
			store := NewIngestionStore(&fakeTransactionalDB{tx: &fakeTx{}})
			store.Now = func() time.Time { return now }
			store.ClaimPlane = &ClaimPlaneFence{Pinner: pinner, Code: claimFenceTestCode, Margin: tc.margin}
			mutation := claimFenceTestMutation("scope-m", "gen-m", "claim-m", 1)
			mutation.LeaseDuration = tc.lease

			ctx, session, err := store.beginClaimFence(context.Background(), mutation, claimFenceTestScope("scope-m"), "gen-m")
			if session != nil {
				_ = session.finish(ctx, "gen-m", nil)
			}
			if !tc.refuse {
				if err != nil {
					t.Fatalf("beginClaimFence() error = %v, want accepted", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "margin") {
				t.Fatalf("beginClaimFence() error = %v, want a margin configuration refusal", err)
			}
			if got := len(pinner.snapshot()); got != 0 {
				t.Fatalf("pin calls = %d, want 0 (refuse before pinning)", got)
			}
		})
	}
}

// blockingStalePinner answers the pre-transaction pin and then holds every
// re-pin until released, ignoring its context, before refusing it as stale:
// a refusal that lands after COMMIT.
type blockingStalePinner struct {
	calls   chan struct{}
	release chan struct{}
	first   bool
}

func (p *blockingStalePinner) PinClaimForCommit(
	context.Context, string, int64, string, string, string, time.Time,
) (string, error) {
	if !p.first {
		p.first = true
		return "commit-a", nil
	}
	p.calls <- struct{}{}
	<-p.release
	return "", claimFenceCodedError{"stale_fence"}
}

// TestPlaneCommitStopsTheKeeperBeforePostCommitWork (#9376 review P3-6): once
// COMMIT returns, a stale re-pin that lands late must not cancel the context
// the post-commit relationship backfill runs on.
func TestPlaneCommitStopsTheKeeperBeforePostCommitWork(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	pinner := &blockingStalePinner{calls: make(chan struct{}, 1), release: make(chan struct{})}
	ticks := make(chan time.Time, 1)
	store := NewIngestionStore(&fakeTransactionalDB{tx: &fakeTx{}})
	store.Now = func() time.Time { return now }
	store.ClaimPlane = &ClaimPlaneFence{
		Pinner: pinner, Code: claimFenceTestCode,
		Ticks: func(time.Duration) (<-chan time.Time, func()) { return ticks, func() {} },
	}
	commitCtx, session, err := store.beginClaimFence(context.Background(),
		claimFenceTestMutation("scope-p", "gen-p", "claim-p", 1), claimFenceTestScope("scope-p"), "gen-p")
	if err != nil {
		t.Fatalf("beginClaimFence() error = %v", err)
	}
	ticks <- time.Time{}
	<-pinner.calls
	// COMMIT has returned here. The late refusal arrives while the store
	// stops the keeper.
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(pinner.release)
	}()
	postCommit := session.afterCommit()
	if postCommit.Err() != nil {
		t.Fatalf("post-commit context error = %v right after the keeper stopped", context.Cause(postCommit))
	}
	if err := session.finish(commitCtx, "gen-p", nil); err != nil {
		t.Fatalf("finish() error = %v, want nil after a durable commit", err)
	}
	if postCommit.Err() != nil {
		t.Fatalf("post-commit context error = %v, want live (a late re-pin must not cancel post-commit work)", context.Cause(postCommit))
	}
}

// hungPinner blocks every call until its context ends.
type hungPinner struct {
	mu    sync.Mutex
	calls []time.Time
}

func (p *hungPinner) PinClaimForCommit(
	ctx context.Context, _ string, _ int64, _, _, _ string, deadline time.Time,
) (string, error) {
	p.mu.Lock()
	p.calls = append(p.calls, deadline)
	p.mu.Unlock()
	<-ctx.Done()
	return "", ctx.Err()
}

// TestPlaneCommitHungPinTimesOutThenUnavailable is the D7 transport-timeout
// row on a hang (#9376 review P2-2): each attempt is bounded, three identical
// attempts run, the commit fails retryable as pin_unavailable, and no core
// transaction opens.
func TestPlaneCommitHungPinTimesOutThenUnavailable(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	database := &fakeTransactionalDB{tx: &fakeTx{}}
	pinner := &hungPinner{}
	store := NewIngestionStore(database)
	store.Now = func() time.Time { return now }
	store.ClaimPlane = &ClaimPlaneFence{
		Pinner: pinner, Code: claimFenceTestCode, PinBackoff: time.Microsecond, PinAttemptTimeout: 20 * time.Millisecond,
	}
	done := make(chan error, 1)
	go func() {
		done <- store.CommitClaimedScopeGeneration(context.Background(),
			claimFenceTestMutation("scope-h", "gen-h", "claim-h", 2),
			claimFenceTestScope("scope-h"), claimFenceTestGeneration("scope-h", "gen-h", now), closedFactStream())
	}()
	var err error
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("commit did not return within 5s against a hung plane")
	}
	var rejection *fence.Rejection
	if !errors.As(err, &rejection) || rejection.Reason != fence.ReasonPinUnavailable || rejection.TerminalFailure() {
		t.Fatalf("commit error = %v, want retryable pin_unavailable", err)
	}
	if database.beginCalls != 0 {
		t.Fatalf("begin calls = %d, want 0", database.beginCalls)
	}
	pinner.mu.Lock()
	defer pinner.mu.Unlock()
	if len(pinner.calls) != 3 || pinner.calls[0] != pinner.calls[1] || pinner.calls[1] != pinner.calls[2] {
		t.Fatalf("pin deadlines = %v, want 3 identical attempts", pinner.calls)
	}
}

// TestClaimFenceMismatchLogsAtError: an equal token under another claim is an
// integrity alarm, so it logs at ERROR; a routine refusal logs at WARN.
func TestClaimFenceMismatchLogsAtError(t *testing.T) {
	t.Parallel()
	for reason, want := range map[fence.Reason]slog.Level{
		fence.ReasonFenceClaimMismatch: slog.LevelError,
		fence.ReasonFenceToken:         slog.LevelWarn,
	} {
		var buffer bytes.Buffer
		store := NewIngestionStore(&fakeTransactionalDB{tx: &fakeTx{}})
		store.Logger = slog.New(slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
		session := &claimFenceSession{store: store, config: &ClaimPlaneFence{}}
		session.recordRejection(context.Background(), &fence.Rejection{Reason: reason}, "gen-l")
		var record struct {
			Level string `json:"level"`
		}
		if err := json.Unmarshal(buffer.Bytes(), &record); err != nil {
			t.Fatalf("%s: decode log %q: %v", reason, buffer.String(), err)
		}
		if record.Level != want.String() {
			t.Fatalf("%s logged at %s, want %s", reason, record.Level, want)
		}
	}
}

// TestIngestionStoreReportsClaimPlaneFenced is the store half of the #9376 D8
// construction check: only a ClaimPlane with a Pinner counts as fenced, so a
// claim-plane control store paired with a fence that has no pinner is refused
// by collector.ClaimedService before any claim.
func TestIngestionStoreReportsClaimPlaneFenced(t *testing.T) {
	t.Parallel()
	store := NewIngestionStore(&fakeTransactionalDB{tx: &fakeTx{}})
	if store.ClaimPlaneFenced() {
		t.Fatal("ClaimPlaneFenced() = true with no ClaimPlane")
	}
	store.ClaimPlane = &ClaimPlaneFence{}
	if store.ClaimPlaneFenced() {
		t.Fatal("ClaimPlaneFenced() = true with a ClaimPlane that has no Pinner")
	}
	store.ClaimPlane.Pinner = &claimFenceFakePinner{}
	if !store.ClaimPlaneFenced() {
		t.Fatal("ClaimPlaneFenced() = false with a pinned ClaimPlane")
	}
}

// TestClaimFenceRecordsOnItsOwnInstruments: collectors that leave the store's
// Instruments unset still get the fence counters through
// ClaimPlaneFence.Instruments.
func TestClaimFenceRecordsOnItsOwnInstruments(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("claim-fence-own"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	store := NewIngestionStore(&fakeTransactionalDB{tx: &fakeTx{}})
	session := &claimFenceSession{store: store, config: &ClaimPlaneFence{Instruments: inst}}
	session.recordRejection(context.Background(), &fence.Rejection{Reason: fence.ReasonFenceToken}, "gen-o")
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name == "eshu_dp_ingestion_claim_fence_rejections_total" {
				return
			}
		}
	}
	t.Fatal("eshu_dp_ingestion_claim_fence_rejections_total not recorded on ClaimPlaneFence.Instruments")
}
