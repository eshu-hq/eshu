// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/ghactionsruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/runwatermark"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	routedGitHubScope = "ci-cd:github-actions:example/repo"
	routedGitLabScope = "gitlab-ci://gitlab.com/eshu-hq/demo"
)

// routedRunClient serves one fetched window to the real GitHub source.
type routedRunClient struct{ newestRunID string }

func (c routedRunClient) FetchRuns(context.Context, ghactionsruntime.TargetConfig) (ghactionsruntime.RunPage, error) {
	return ghactionsruntime.RunPage{Snapshots: []ghactionsruntime.RunSnapshot{{
		Run: map[string]any{
			"id":         c.newestRunID,
			"head_sha":   "0123456789abcdef0123456789abcdef01234567",
			"repository": map[string]any{"full_name": "example/repo"},
		},
	}}}, nil
}

// routedCommitter applies mark on the commit context and stores nothing, the
// way IngestionStore marks the box from inside its commit path.
type routedCommitter struct{ mark func(context.Context) }

func (c routedCommitter) CommitScopeGeneration(context.Context, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error {
	return errors.New("unclaimed commit not expected")
}

func (c routedCommitter) CommitClaimedScopeGeneration(
	ctx context.Context, _ workflow.ClaimMutation, _ scope.IngestionScope, _ scope.ScopeGeneration, stream <-chan facts.Envelope,
) error {
	for range stream {
	}
	c.mark(ctx)
	return nil
}

// routedClaimStore hands out one claim, then stops the service on completion.
type routedClaimStore struct {
	item      workflow.WorkItem
	claimed   bool
	completed int
	cancel    context.CancelFunc
}

func (s *routedClaimStore) ClaimNextEligible(context.Context, workflow.ClaimSelector, time.Time, time.Duration) (workflow.WorkItem, workflow.Claim, bool, error) {
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

func (s *routedClaimStore) HeartbeatClaim(context.Context, workflow.ClaimMutation) error {
	return nil
}

func (s *routedClaimStore) CompleteClaim(context.Context, workflow.ClaimMutation) error {
	s.completed++
	s.cancel()
	return nil
}

func (s *routedClaimStore) ReleaseClaim(context.Context, workflow.ClaimMutation) error {
	s.cancel()
	return nil
}

func (s *routedClaimStore) FailClaimRetryable(context.Context, workflow.ClaimMutation) error {
	s.cancel()
	return errors.New("claim failed retryable")
}

func (s *routedClaimStore) FailClaimTerminal(context.Context, workflow.ClaimMutation) error {
	s.cancel()
	return errors.New("claim failed terminal")
}

// TestRoutedClaimAppliesTheWatermarkPerCommitDisposition runs the production
// shape, ClaimedService over providerRoutedSource over the real GitHub source.
// ClaimedService finds both hooks by type assertion on its Source, so a router
// that hides them leaves the watermark unwritten in this binary (#10063; the
// router has hidden them since #5778).
func TestRoutedClaimAppliesTheWatermarkPerCommitDisposition(t *testing.T) {
	t.Parallel()

	refused := collector.CommitRefusal{Reason: "destructive_successor_refused", Rule: "collector_ratio"}
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

			target := ghactionsruntime.TargetConfig{
				ScopeID: routedGitHubScope, Repository: "example/repo", Token: "token",
				AllowedRepositories: []string{"example/repo"}, MaxRuns: 10, MaxJobs: 10, MaxArtifacts: 10,
			}
			key := runwatermark.Key{ScopeID: target.ScopeID, Repository: target.Repository}
			store := runwatermark.NewInMemoryStore()
			if err := store.Save(context.Background(), runwatermark.Watermark{
				Key: key, LastRunID: "100", GenerationID: "generation-0", FencingToken: 1,
			}); err != nil {
				t.Fatalf("seed watermark: %v", err)
			}
			github, err := ghactionsruntime.NewClaimedSource(ghactionsruntime.SourceConfig{
				CollectorInstanceID: "ci-cd-primary",
				Client:              routedRunClient{newestRunID: "130"},
				Watermarks:          store,
				Targets:             []ghactionsruntime.TargetConfig{target},
			})
			if err != nil {
				t.Fatalf("NewClaimedSource() error = %v", err)
			}
			routed, err := newProviderRoutedSource(map[string]collector.ClaimedSource{routedGitHubScope: github})
			if err != nil {
				t.Fatalf("newProviderRoutedSource() error = %v", err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			control := &routedClaimStore{cancel: cancel, item: workflow.WorkItem{
				WorkItemID: "work-1", CollectorKind: scope.CollectorCICDRun, CollectorInstanceID: "ci-cd-primary",
				SourceSystem: string(scope.CollectorCICDRun), ScopeID: routedGitHubScope, AcceptanceUnitID: routedGitHubScope,
				SourceRunID: "generation-1", GenerationID: "generation-1", CurrentClaimID: "claim-1",
				CurrentFencingToken: 2, CurrentOwnerID: "worker-1",
			}}
			service := collector.ClaimedService{
				ControlStore: control, Source: routed, Committer: routedCommitter{mark: tc.mark},
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
			got, ok, err := store.Load(context.Background(), key)
			if err != nil || !ok {
				t.Fatalf("Load() = %+v, %v, %v", got, ok, err)
			}
			if got.LastRunID != tc.wantWatermark {
				t.Fatalf("watermark LastRunID = %q, want %q", got.LastRunID, tc.wantWatermark)
			}
		})
	}
}

// observingRoutedSource records what the router forwards to it.
type observingRoutedSource struct {
	fakeRoutedSource
	committed []string
	refused   []collector.CommitRefusal
	err       error
}

func (s *observingRoutedSource) ObserveClaimedGenerationCommitted(_ context.Context, item workflow.WorkItem) error {
	s.committed = append(s.committed, item.ScopeID)
	return s.err
}

func (s *observingRoutedSource) ObserveClaimedGenerationRefused(_ context.Context, _ workflow.WorkItem, refusal collector.CommitRefusal) error {
	s.refused = append(s.refused, refusal)
	return s.err
}

// TestProviderRoutedSourceForwardsObserversByScope pins the routing rows: a hook
// reaches only the claim's provider, a provider without hooks (GitLab has none)
// is a no-op, a source's error comes back, and an unregistered scope is an error
// the service counts rather than a silent drop.
func TestProviderRoutedSourceForwardsObserversByScope(t *testing.T) {
	t.Parallel()

	refusal := collector.CommitRefusal{Reason: "repository_retiring"}
	wantErr := errors.New("watermark store unavailable")
	github := &observingRoutedSource{}
	failing := &observingRoutedSource{err: wantErr}
	routed, err := newProviderRoutedSource(map[string]collector.ClaimedSource{
		routedGitHubScope: github,
		routedGitLabScope: &fakeRoutedSource{},
		"failing":         failing,
	})
	if err != nil {
		t.Fatalf("newProviderRoutedSource() error = %v", err)
	}
	ctx := context.Background()

	if err := routed.ObserveClaimedGenerationCommitted(ctx, workflow.WorkItem{ScopeID: routedGitHubScope}); err != nil {
		t.Fatalf("committed(github) error = %v", err)
	}
	if err := routed.ObserveClaimedGenerationRefused(ctx, workflow.WorkItem{ScopeID: routedGitHubScope}, refusal); err != nil {
		t.Fatalf("refused(github) error = %v", err)
	}
	if len(github.committed) != 1 || github.committed[0] != routedGitHubScope {
		t.Fatalf("github committed = %v, want one call for %q", github.committed, routedGitHubScope)
	}
	if len(github.refused) != 1 || github.refused[0] != refusal {
		t.Fatalf("github refused = %v, want [%+v]", github.refused, refusal)
	}

	if err := routed.ObserveClaimedGenerationCommitted(ctx, workflow.WorkItem{ScopeID: routedGitLabScope}); err != nil {
		t.Fatalf("committed(gitlab, no hook) error = %v, want nil", err)
	}
	if err := routed.ObserveClaimedGenerationRefused(ctx, workflow.WorkItem{ScopeID: routedGitLabScope}, refusal); err != nil {
		t.Fatalf("refused(gitlab, no hook) error = %v, want nil", err)
	}
	if len(github.committed) != 1 || len(github.refused) != 1 {
		t.Fatalf("a gitlab claim reached the github source: %d committed, %d refused", len(github.committed), len(github.refused))
	}

	if err := routed.ObserveClaimedGenerationCommitted(ctx, workflow.WorkItem{ScopeID: "failing"}); !errors.Is(err, wantErr) {
		t.Fatalf("committed(failing) error = %v, want %v", err, wantErr)
	}
	if err := routed.ObserveClaimedGenerationRefused(ctx, workflow.WorkItem{ScopeID: "failing"}, refusal); !errors.Is(err, wantErr) {
		t.Fatalf("refused(failing) error = %v, want %v", err, wantErr)
	}

	const unknown = "gitlab-ci://gitlab.com/unregistered/project"
	for name, call := range map[string]func() error{
		"committed": func() error {
			return routed.ObserveClaimedGenerationCommitted(ctx, workflow.WorkItem{ScopeID: unknown})
		},
		"refused": func() error {
			return routed.ObserveClaimedGenerationRefused(ctx, workflow.WorkItem{ScopeID: unknown}, refusal)
		},
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), unknown) {
			t.Fatalf("%s(unregistered) error = %v, want one naming %q", name, err, unknown)
		}
	}
}
