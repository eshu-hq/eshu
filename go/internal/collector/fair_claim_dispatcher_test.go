// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestFairClaimDispatcherSkipsEmptyTargetsWithoutStarvingFamilies(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 16, 22, 30, 0, 0, time.UTC)
	gitItem := testClaimedWorkItem(now)
	gitItem.CollectorKind = scope.CollectorGit
	gitItem.CollectorInstanceID = "git-primary"
	store := &fairDispatcherStore{
		results: map[string][]fairDispatcherClaimResult{
			"aws:aws-primary": {},
			"git:git-primary": {{
				item:  gitItem,
				claim: testWorkflowClaim(gitItem.WorkItemID, now),
				found: true,
			}},
		},
	}
	dispatcher, err := NewFairClaimDispatcher(store, []workflow.FairnessCandidate{
		{CollectorKind: scope.CollectorAWS, CollectorInstanceID: "aws-primary", Weight: 1},
		{CollectorKind: scope.CollectorGit, CollectorInstanceID: "git-primary", Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewFairClaimDispatcher() error = %v, want nil", err)
	}

	item, _, target, found, err := dispatcher.ClaimNext(
		context.Background(),
		"owner-1",
		"claim-1",
		now,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("ClaimNext() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNext() found = false, want true after skipping empty target")
	}
	if got, want := item.CollectorKind, scope.CollectorGit; got != want {
		t.Fatalf("WorkItem.CollectorKind = %q, want %q", got, want)
	}
	if got, want := target.CollectorKind, scope.CollectorGit; got != want {
		t.Fatalf("target collector kind = %q, want %q", got, want)
	}
	wantSelectors := []workflow.ClaimTarget{
		{CollectorKind: scope.CollectorAWS, CollectorInstanceID: "aws-primary"},
		{CollectorKind: scope.CollectorGit, CollectorInstanceID: "git-primary"},
	}
	if fmt.Sprint(store.selectors) != fmt.Sprint(wantSelectors) {
		t.Fatalf("claim selectors = %#v, want %#v", store.selectors, wantSelectors)
	}
}

func TestFairClaimDispatcherSerializesSchedulerState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 16, 23, 0, 0, 0, time.UTC)
	store := &fairDispatcherStore{
		results: map[string][]fairDispatcherClaimResult{},
	}
	dispatcher, err := NewFairClaimDispatcher(store, []workflow.FairnessCandidate{
		{CollectorKind: scope.CollectorAWS, CollectorInstanceID: "aws-primary", Weight: 1},
		{CollectorKind: scope.CollectorGit, CollectorInstanceID: "git-primary", Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewFairClaimDispatcher() error = %v, want nil", err)
	}

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for attempt := 0; attempt < 20; attempt++ {
				_, _, _, found, err := dispatcher.ClaimNext(
					context.Background(),
					"owner-1",
					fmt.Sprintf("claim-%d-%d", worker, attempt),
					now,
					time.Minute,
				)
				if err != nil {
					t.Errorf("ClaimNext() error = %v, want nil", err)
				}
				if found {
					t.Error("ClaimNext() found = true, want false for empty targets")
				}
			}
		}(worker)
	}
	wg.Wait()

	if got, want := store.selectorCount(), 8*20*2; got != want {
		t.Fatalf("selector count = %d, want %d", got, want)
	}
}

func TestClaimedServiceUsesFairDispatcherAndPreservesClaimLifecycle(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.June, 16, 22, 45, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.CollectorKind = scope.CollectorAWS
	item.CollectorInstanceID = "aws-primary"
	item.SourceSystem = "aws"
	item.ScopeID = "aws-scope-1"
	item.AcceptanceUnitID = "aws-account-1"
	item.SourceRunID = "aws-generation-1"
	item.GenerationID = "aws-generation-1"
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &fairDispatcherStore{
		results: map[string][]fairDispatcherClaimResult{
			"aws:aws-primary": {{item: item, claim: claim, found: true}},
		},
		heartbeat: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	dispatcher, err := NewFairClaimDispatcher(store, []workflow.FairnessCandidate{
		{CollectorKind: scope.CollectorAWS, CollectorInstanceID: "aws-primary", Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewFairClaimDispatcher() error = %v, want nil", err)
	}
	sourceScope := testScope()
	sourceScope.CollectorKind = scope.CollectorAWS
	sourceScope.SourceSystem = "aws"
	sourceScope.ScopeID = item.ScopeID
	generation := testGeneration(now)
	generation.ScopeID = item.ScopeID
	generation.GenerationID = item.GenerationID
	source := &stubClaimedSource{
		collected: FactsFromSlice(sourceScope, generation, testFacts(now)),
		ok:        true,
	}
	committer := &stubClaimedCommitter{}
	service := testClaimedService(now, claim, scope.CollectorGit, nil, source, committer)
	service.ControlStore = store
	service.ClaimDispatcher = dispatcher
	service.CollectorKind = ""
	service.CollectorInstanceID = ""

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := store.completeCalls, 1; got != want {
		t.Fatalf("complete calls = %d, want %d", got, want)
	}
	if got, want := committer.claimedCalls, 1; got != want {
		t.Fatalf("claimed commit calls = %d, want %d", got, want)
	}
	if got := store.lastComplete; got.WorkItemID != item.WorkItemID ||
		got.ClaimID != claim.ClaimID ||
		got.FencingToken != claim.FencingToken {
		t.Fatalf("complete mutation = %#v, want dispatched claim fence", got)
	}
}

type fairDispatcherClaimResult struct {
	item  workflow.WorkItem
	claim workflow.Claim
	found bool
	err   error
}

type fairDispatcherStore struct {
	mu            sync.Mutex
	results       map[string][]fairDispatcherClaimResult
	selectors     []workflow.ClaimTarget
	completeCalls int
	lastComplete  workflow.ClaimMutation
	heartbeat     func(context.Context, workflow.ClaimMutation) error
}

func (s *fairDispatcherStore) ClaimNextEligible(
	ctx context.Context,
	selector workflow.ClaimSelector,
	now time.Time,
	leaseTTL time.Duration,
) (workflow.WorkItem, workflow.Claim, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selectors = append(s.selectors, workflow.ClaimTarget{
		CollectorKind:       selector.CollectorKind,
		CollectorInstanceID: selector.CollectorInstanceID,
	})
	key := string(selector.CollectorKind) + ":" + selector.CollectorInstanceID
	queue := s.results[key]
	if len(queue) == 0 {
		return workflow.WorkItem{}, workflow.Claim{}, false, nil
	}
	next := queue[0]
	s.results[key] = queue[1:]
	return next.item, next.claim, next.found, next.err
}

func (s *fairDispatcherStore) selectorCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.selectors)
}

func (s *fairDispatcherStore) HeartbeatClaim(ctx context.Context, mutation workflow.ClaimMutation) error {
	if s.heartbeat != nil {
		return s.heartbeat(ctx, mutation)
	}
	return nil
}

func (s *fairDispatcherStore) CompleteClaim(_ context.Context, mutation workflow.ClaimMutation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completeCalls++
	s.lastComplete = mutation
	return nil
}

func (s *fairDispatcherStore) ReleaseClaim(context.Context, workflow.ClaimMutation) error {
	return nil
}

func (s *fairDispatcherStore) FailClaimRetryable(context.Context, workflow.ClaimMutation) error {
	return nil
}

func (s *fairDispatcherStore) FailClaimTerminal(context.Context, workflow.ClaimMutation) error {
	return nil
}
