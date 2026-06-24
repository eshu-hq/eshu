// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

func TestClaimedServiceRoutesExtensionResultsThroughWorkflowMutations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		result           func(workflow.WorkItem) sdkcollector.Result
		wantCommits      int
		wantComplete     int
		wantRetryable    int
		wantTerminal     int
		wantFailureClass string
	}{
		{
			name: "complete",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				return completeResult(item, testSDKFact(item))
			},
			wantCommits:  1,
			wantComplete: 1,
		},
		{
			name: "partial",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := baseResult(item, sdkcollector.ResultPartial)
				result.Facts = []sdkcollector.Fact{testSDKFact(item)}
				result.Statuses = []sdkcollector.Status{{
					Class:   sdkcollector.StatusWarning,
					Partial: true,
				}}
				return result
			},
			wantCommits:  1,
			wantComplete: 1,
		},
		{
			name: "unchanged",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := baseResult(item, sdkcollector.ResultUnchanged)
				result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusComplete}}
				return result
			},
			wantComplete: 1,
		},
		{
			name: "retryable",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := baseResult(item, sdkcollector.ResultRetryable)
				result.Statuses = []sdkcollector.Status{{
					Class:             sdkcollector.StatusFailure,
					FailureClass:      "rate_limited",
					RetryAfterSeconds: 15,
				}}
				return result
			},
			wantRetryable:    1,
			wantFailureClass: "rate_limited",
		},
		{
			name: "terminal",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := baseResult(item, sdkcollector.ResultTerminal)
				result.Statuses = []sdkcollector.Status{{
					Class:        sdkcollector.StatusFailure,
					FailureClass: "invalid_config",
				}}
				return result
			},
			wantTerminal:     1,
			wantFailureClass: "invalid_config",
		},
		{
			name: "invalid result",
			result: func(item workflow.WorkItem) sdkcollector.Result {
				result := completeResult(item, testSDKFact(item))
				result.Facts[0].Kind = "dev.example.undeclared"
				return result
			},
			wantTerminal:     1,
			wantFailureClass: FailureClassInvalidResult,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			item := testWorkItem()
			ctx, cancel := context.WithCancel(context.Background())
			store := &serviceClaimStore{
				item:   item,
				claim:  testWorkflowClaim(item),
				cancel: cancel,
			}
			committer := &serviceCommitter{}
			source := mustNewSource(t, &recordingRunner{result: tc.result(item)}, nil)
			service := collector.ClaimedService{
				ControlStore:        store,
				Source:              source,
				Committer:           committer,
				CollectorKind:       item.CollectorKind,
				CollectorInstanceID: item.CollectorInstanceID,
				OwnerID:             item.CurrentOwnerID,
				ClaimIDFunc: func() string {
					return item.CurrentClaimID
				},
				PollInterval:      time.Millisecond,
				ClaimLeaseTTL:     time.Minute,
				HeartbeatInterval: 30 * time.Second,
				Clock:             testObservedAt,
			}

			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if got := committer.commits; got != tc.wantCommits {
				t.Fatalf("commits = %d, want %d", got, tc.wantCommits)
			}
			if got := store.completeCalls; got != tc.wantComplete {
				t.Fatalf("CompleteClaim calls = %d, want %d", got, tc.wantComplete)
			}
			if got := store.retryableCalls; got != tc.wantRetryable {
				t.Fatalf("FailClaimRetryable calls = %d, want %d", got, tc.wantRetryable)
			}
			if got := store.terminalCalls; got != tc.wantTerminal {
				t.Fatalf("FailClaimTerminal calls = %d, want %d", got, tc.wantTerminal)
			}
			if got := store.failureClass; got != tc.wantFailureClass {
				t.Fatalf("failure class = %q, want %q", got, tc.wantFailureClass)
			}
		})
	}
}

type serviceClaimStore struct {
	item           workflow.WorkItem
	claim          workflow.Claim
	cancel         context.CancelFunc
	claimed        bool
	completeCalls  int
	retryableCalls int
	terminalCalls  int
	failureClass   string
}

func (s *serviceClaimStore) ClaimNextEligible(
	context.Context,
	workflow.ClaimSelector,
	time.Time,
	time.Duration,
) (workflow.WorkItem, workflow.Claim, bool, error) {
	if s.claimed {
		return workflow.WorkItem{}, workflow.Claim{}, false, nil
	}
	s.claimed = true
	return s.item, s.claim, true, nil
}

func (s *serviceClaimStore) HeartbeatClaim(context.Context, workflow.ClaimMutation) error {
	return nil
}

func (s *serviceClaimStore) CompleteClaim(_ context.Context, mutation workflow.ClaimMutation) error {
	s.completeCalls++
	s.failureClass = mutation.FailureClass
	s.cancel()
	return nil
}

func (s *serviceClaimStore) ReleaseClaim(context.Context, workflow.ClaimMutation) error {
	s.cancel()
	return nil
}

func (s *serviceClaimStore) FailClaimRetryable(_ context.Context, mutation workflow.ClaimMutation) error {
	s.retryableCalls++
	s.failureClass = mutation.FailureClass
	s.cancel()
	return nil
}

func (s *serviceClaimStore) FailClaimTerminal(_ context.Context, mutation workflow.ClaimMutation) error {
	s.terminalCalls++
	s.failureClass = mutation.FailureClass
	s.cancel()
	return nil
}

type serviceCommitter struct {
	commits int
}

func (s *serviceCommitter) CommitScopeGeneration(
	context.Context,
	scope.IngestionScope,
	scope.ScopeGeneration,
	<-chan facts.Envelope,
) error {
	return nil
}

func (s *serviceCommitter) CommitClaimedScopeGeneration(
	_ context.Context,
	_ workflow.ClaimMutation,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	envelopes <-chan facts.Envelope,
) error {
	for range envelopes {
	}
	s.commits++
	return nil
}

func testWorkflowClaim(item workflow.WorkItem) workflow.Claim {
	now := testObservedAt()
	return workflow.Claim{
		ClaimID:        item.CurrentClaimID,
		WorkItemID:     item.WorkItemID,
		FencingToken:   item.CurrentFencingToken,
		OwnerID:        item.CurrentOwnerID,
		Status:         workflow.ClaimStatusActive,
		ClaimedAt:      now.Add(-time.Second),
		HeartbeatAt:    now.Add(-time.Second),
		LeaseExpiresAt: item.LeaseExpiresAt,
		CreatedAt:      now.Add(-time.Second),
		UpdatedAt:      now,
	}
}
