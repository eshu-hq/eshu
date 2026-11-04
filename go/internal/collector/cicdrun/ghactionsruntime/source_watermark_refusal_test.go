// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/runwatermark"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// refusingCommitter marks the commit's disposition the way IngestionStore does
// and stores nothing.
type refusingCommitter struct {
	mark func(context.Context)
}

func (c refusingCommitter) CommitScopeGeneration(context.Context, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error {
	return errors.New("unclaimed commit not expected")
}

func (c refusingCommitter) CommitClaimedScopeGeneration(
	ctx context.Context, _ workflow.ClaimMutation, _ scope.IngestionScope, _ scope.ScopeGeneration, stream <-chan facts.Envelope,
) error {
	for range stream {
	}
	c.mark(ctx)
	return nil
}

// watermarkClaimStore is the ClaimedService control store for one claim.
type watermarkClaimStore struct {
	item      workflow.WorkItem
	claimed   bool
	completed int
	cancel    context.CancelFunc
}

func (s *watermarkClaimStore) ClaimNextEligible(context.Context, workflow.ClaimSelector, time.Time, time.Duration) (workflow.WorkItem, workflow.Claim, bool, error) {
	if s.claimed {
		return workflow.WorkItem{}, workflow.Claim{}, false, nil
	}
	s.claimed = true
	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	return s.item, workflow.Claim{
		ClaimID: s.item.CurrentClaimID, WorkItemID: s.item.WorkItemID, FencingToken: s.item.CurrentFencingToken,
		OwnerID: s.item.CurrentOwnerID, Status: workflow.ClaimStatusActive, ClaimedAt: now,
		HeartbeatAt: now, LeaseExpiresAt: now.Add(time.Minute), CreatedAt: now, UpdatedAt: now,
	}, true, nil
}

func (s *watermarkClaimStore) HeartbeatClaim(context.Context, workflow.ClaimMutation) error {
	return nil
}

func (s *watermarkClaimStore) CompleteClaim(context.Context, workflow.ClaimMutation) error {
	s.completed++
	s.cancel()
	return nil
}

func (s *watermarkClaimStore) ReleaseClaim(context.Context, workflow.ClaimMutation) error {
	s.cancel()
	return nil
}

func (s *watermarkClaimStore) FailClaimRetryable(context.Context, workflow.ClaimMutation) error {
	s.cancel()
	return errors.New("claim failed retryable")
}

func (s *watermarkClaimStore) FailClaimTerminal(context.Context, workflow.ClaimMutation) error {
	s.cancel()
	return errors.New("claim failed terminal")
}

// TestWatermarkHoldsPerCommitDisposition is the watermark hold row of #10037.
// A refused commit stored none of the cycle's runs, so the watermark must stay,
// or the next cycle would stop detecting the gap those runs left. A committed
// or unchanged-skip cycle advances it, as before. It drives the real
// ClaimedService over this source, so the row proves the service's routing and
// the source's hook. It does not go through providerRoutedSource, so it says
// nothing about the collector-cicd-run binary, which reaches neither hook (#10063).
func TestWatermarkHoldsPerCommitDisposition(t *testing.T) {
	t.Parallel()

	refused := collector.CommitRefusal{
		Reason: telemetry.RefreshSkipReasonDestructiveSuccessor,
		Rule:   telemetry.SuccessorGuardRuleCollectorRatio,
	}
	tests := []struct {
		name          string
		mark          func(context.Context)
		wantWatermark string
	}{
		{name: "committed", mark: func(context.Context) {}, wantWatermark: "130"},
		{name: "unchanged skip", mark: collector.MarkCommitUnchangedSkip, wantWatermark: "130"},
		{
			name:          "refused",
			mark:          func(ctx context.Context) { collector.MarkCommitRefused(ctx, refused) },
			wantWatermark: "100",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			target := watermarkTestTarget()
			store := runwatermark.NewInMemoryStore()
			if err := store.Save(context.Background(), runwatermark.Watermark{
				Key: watermarkKey(target), LastRunID: "100", GenerationID: "generation-0", FencingToken: 1,
			}); err != nil {
				t.Fatalf("seed watermark: %v", err)
			}
			source, err := NewClaimedSource(SourceConfig{
				CollectorInstanceID: "ci-cd-primary",
				Client:              &sequencedClient{pages: []RunPage{{Snapshots: []RunSnapshot{minimalRunSnapshot("130")}}}},
				Watermarks:          store,
				Targets:             []TargetConfig{target},
			})
			if err != nil {
				t.Fatalf("NewClaimedSource() error = %v", err)
			}

			item := watermarkTestClaim("generation-1", 2)
			item.WorkItemID, item.CurrentClaimID, item.CurrentOwnerID = "work-1", "claim-1", "worker-1"
			item.SourceSystem, item.SourceRunID = string(scope.CollectorCICDRun), item.GenerationID
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			control := &watermarkClaimStore{item: item, cancel: cancel}
			service := collector.ClaimedService{
				ControlStore: control, Source: source, Committer: refusingCommitter{mark: tc.mark},
				CollectorKind: scope.CollectorCICDRun, CollectorInstanceID: "ci-cd-primary",
				OwnerID: "worker-1", ClaimIDFunc: func() string { return "claim-1" },
				PollInterval: time.Minute, ClaimLeaseTTL: time.Minute, HeartbeatInterval: 30 * time.Second,
				MaxAttempts: 3, Clock: func() time.Time { return time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC) },
			}
			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if control.completed != 1 {
				t.Fatalf("completed = %d, want 1", control.completed)
			}
			got, ok, err := store.Load(context.Background(), watermarkKey(target))
			if err != nil || !ok {
				t.Fatalf("Load() = %+v, %v, %v", got, ok, err)
			}
			if got.LastRunID != tc.wantWatermark {
				t.Fatalf("watermark LastRunID = %q, want %q", got.LastRunID, tc.wantWatermark)
			}
			if _, pending := source.pending.takeFor(item); pending {
				t.Fatal("the cycle's stashed watermark is still pending: a recurring refusal would leak one entry per generation")
			}
		})
	}
}
