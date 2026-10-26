// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// errHeartbeatLostForTest simulates a fencing loss on the dispatch grant.
var errHeartbeatLostForTest = errors.New("heartbeat lost for test")

// itemRecordingClaimedSource captures the work item handed to dispatch.
type itemRecordingClaimedSource struct {
	collected CollectedGeneration
	ok        bool
	got       []workflow.WorkItem
	cancel    context.CancelFunc
}

func (s *itemRecordingClaimedSource) NextClaimed(_ context.Context, item workflow.WorkItem) (CollectedGeneration, bool, error) {
	s.got = append(s.got, item)
	if s.cancel != nil {
		s.cancel()
	}
	return s.collected, s.ok, nil
}

// TestClaimedServiceRestampsStaleLeaseAtDispatch is the RED test for
// eshu-hq/eshu#9205: the item handed to dispatch must carry the live
// heartbeat-granted lease, never a frozen claim-time snapshot. A stale
// LeaseExpiresAt ships a dead deadline to the satellite, which fails
// closed on every run.
func TestClaimedServiceRestampsStaleLeaseAtDispatch(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.October, 26, 19, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	// Frozen snapshot: the lease the claim row carried two cycles ago.
	item.LeaseExpiresAt = now.Add(-time.Minute)
	claim := testWorkflowClaim(item.WorkItemID, now)
	var heartbeats []workflow.ClaimMutation
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		heartbeat: func(_ context.Context, mutation workflow.ClaimMutation) error {
			heartbeats = append(heartbeats, mutation)
			return nil
		},
	}
	source := &itemRecordingClaimedSource{
		collected: FactsFromSlice(testScope(), testGeneration(now), testFacts(now)),
		ok:        true,
	}
	committer := &stubClaimedCommitter{}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, committer)
	source.cancel = cancel

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if len(source.got) != 1 {
		t.Fatalf("dispatches = %d, want 1", len(source.got))
	}
	if len(heartbeats) == 0 {
		t.Fatal("no heartbeat recorded, want at least the dispatch grant")
	}
	granted := heartbeats[len(heartbeats)-1].ObservedAt.Add(time.Minute)
	if got := source.got[0].LeaseExpiresAt; !got.Equal(granted) {
		t.Fatalf("dispatched lease = %v, want heartbeat-granted %v", got, granted)
	}
}

// TestClaimedServiceSkipsDispatchWhenDispatchHeartbeatFails proves fencing:
// a lost grant never reaches the satellite.
func TestClaimedServiceSkipsDispatchWhenDispatchHeartbeatFails(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.October, 26, 19, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.LeaseExpiresAt = now.Add(-time.Minute)
	claim := testWorkflowClaim(item.WorkItemID, now)
	calls := 0
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		heartbeat: func(_ context.Context, _ workflow.ClaimMutation) error {
			calls++
			if calls == 2 {
				return errHeartbeatLostForTest
			}
			return nil
		},
		retryableFail: func(_ context.Context, _ workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &itemRecordingClaimedSource{
		collected: FactsFromSlice(testScope(), testGeneration(now), testFacts(now)),
		ok:        true,
	}
	committer := &stubClaimedCommitter{}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, committer)
	source.cancel = cancel

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil (lost grant fails the claim, not the loop)", err)
	}
	if len(source.got) != 0 {
		t.Fatalf("dispatches = %d, want 0 (lost grant must not dispatch)", len(source.got))
	}
	if store.retryableFailCalls != 1 {
		t.Fatalf("retryable fails = %d, want 1", store.retryableFailCalls)
	}
}
