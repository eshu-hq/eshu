// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func testClaimedWorkItem(now time.Time) workflow.WorkItem {
	return workflow.WorkItem{
		WorkItemID:          "item-claim-1",
		RunID:               "run-claim-1",
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-primary",
		SourceSystem:        "git",
		ScopeID:             "scope-claim-1",
		AcceptanceUnitID:    "repo-claim-1",
		SourceRunID:         "generation-claim-1",
		GenerationID:        "generation-claim-1",
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentClaimID:      "claim-claim-1",
		CurrentFencingToken: 1,
		CurrentOwnerID:      "collector-owner-1",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func testWorkflowClaim(workItemID string, now time.Time) workflow.Claim {
	return workflow.Claim{
		ClaimID:        "claim-claim-1",
		WorkItemID:     workItemID,
		FencingToken:   1,
		OwnerID:        "collector-owner-1",
		Status:         workflow.ClaimStatusActive,
		ClaimedAt:      now,
		HeartbeatAt:    now,
		LeaseExpiresAt: now.Add(time.Minute),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func testScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       "scope-claim-1",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-claim-1",
	}
}

func testGeneration(now time.Time) scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: "generation-claim-1",
		ScopeID:      "scope-claim-1",
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

func testFacts(now time.Time) []facts.Envelope {
	return []facts.Envelope{{
		FactID:        "fact-claim-1",
		ScopeID:       "scope-claim-1",
		GenerationID:  "generation-claim-1",
		FactKind:      "repository",
		StableFactKey: "repository:repo-claim-1",
		ObservedAt:    now,
		Payload:       map[string]any{"graph_id": "repo-claim-1"},
	}}
}

func testClaimedService(
	now time.Time,
	claim workflow.Claim,
	kind scope.CollectorKind,
	store *stubClaimStore,
	source ClaimedSource,
	committer Committer,
) ClaimedService {
	return ClaimedService{
		ControlStore:        store,
		Source:              source,
		Committer:           committer,
		CollectorKind:       kind,
		CollectorInstanceID: testCollectorInstanceID(kind),
		OwnerID:             "collector-owner-1",
		ClaimIDFunc:         func() string { return claim.ClaimID },
		PollInterval:        time.Millisecond,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   time.Second,
		Clock:               func() time.Time { return now },
	}
}

func testCollectorInstanceID(kind scope.CollectorKind) string {
	if kind == scope.CollectorTerraformState {
		return "collector-tfstate-primary"
	}
	return "collector-git-primary"
}

type stubClaimStore struct {
	item               workflow.WorkItem
	claim              workflow.Claim
	found              bool
	claimErr           error
	claimCalls         int
	completeCalls      int
	releaseCalls       int
	retryableFailCalls int
	terminalFailCalls  int
	lastComplete       workflow.ClaimMutation
	lastRetryableFail  workflow.ClaimMutation
	lastTerminalFail   workflow.ClaimMutation
	heartbeat          func(context.Context, workflow.ClaimMutation) error
	release            func(context.Context, workflow.ClaimMutation) error
	retryableFail      func(context.Context, workflow.ClaimMutation) error
	terminalFail       func(context.Context, workflow.ClaimMutation) error
}

func (s *stubClaimStore) ClaimNextEligible(
	context.Context,
	workflow.ClaimSelector,
	time.Time,
	time.Duration,
) (workflow.WorkItem, workflow.Claim, bool, error) {
	s.claimCalls++
	if s.claimErr != nil {
		return workflow.WorkItem{}, workflow.Claim{}, false, s.claimErr
	}
	return s.item, s.claim, s.found, nil
}

func (s *stubClaimStore) HeartbeatClaim(ctx context.Context, mutation workflow.ClaimMutation) error {
	if s.heartbeat != nil {
		return s.heartbeat(ctx, mutation)
	}
	return nil
}

func (s *stubClaimStore) CompleteClaim(_ context.Context, mutation workflow.ClaimMutation) error {
	s.completeCalls++
	s.lastComplete = mutation
	return nil
}

func (s *stubClaimStore) ReleaseClaim(ctx context.Context, mutation workflow.ClaimMutation) error {
	s.releaseCalls++
	if s.release != nil {
		return s.release(ctx, mutation)
	}
	return nil
}

func (s *stubClaimStore) FailClaimRetryable(ctx context.Context, mutation workflow.ClaimMutation) error {
	s.retryableFailCalls++
	s.lastRetryableFail = mutation
	if s.retryableFail != nil {
		return s.retryableFail(ctx, mutation)
	}
	return nil
}

func (s *stubClaimStore) FailClaimTerminal(ctx context.Context, mutation workflow.ClaimMutation) error {
	s.terminalFailCalls++
	s.lastTerminalFail = mutation
	if s.terminalFail != nil {
		return s.terminalFail(ctx, mutation)
	}
	return nil
}

type stubClaimedSource struct {
	collected CollectedGeneration
	ok        bool
	err       error
}

func (s *stubClaimedSource) NextClaimed(context.Context, workflow.WorkItem) (CollectedGeneration, bool, error) {
	return s.collected, s.ok, s.err
}

type stubClaimedCommitter struct {
	claimedCalls                 int
	claimedStreamErrorCalls      int
	lastClaimMutation            workflow.ClaimMutation
	claimedCommit                func(context.Context, workflow.ClaimMutation, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error
	claimedCommitWithStreamError func(context.Context, workflow.ClaimMutation, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope, func() error) error
}

func (s *stubClaimedCommitter) CommitScopeGeneration(
	context.Context,
	scope.IngestionScope,
	scope.ScopeGeneration,
	<-chan facts.Envelope,
) error {
	return errors.New("generic commit should not be used")
}

func (s *stubClaimedCommitter) CommitClaimedScopeGeneration(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	s.claimedCalls++
	s.lastClaimMutation = mutation
	if s.claimedCommit != nil {
		return s.claimedCommit(ctx, mutation, scopeValue, generation, factStream)
	}
	for range factStream {
	}
	return nil
}

func (s *stubClaimedCommitter) CommitClaimedScopeGenerationWithStreamError(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
) error {
	s.claimedStreamErrorCalls++
	s.lastClaimMutation = mutation
	if s.claimedCommitWithStreamError != nil {
		return s.claimedCommitWithStreamError(ctx, mutation, scopeValue, generation, factStream, factStreamErr)
	}
	for range factStream {
	}
	if factStreamErr != nil {
		return factStreamErr()
	}
	return nil
}
