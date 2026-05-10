package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedServiceClaimsHeartbeatsCommitsAndCompletes(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.April, 20, 22, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		heartbeat: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{
		collected: FactsFromSlice(testScope(), testGeneration(now), testFacts(now)),
		ok:        true,
	}
	committer := &stubClaimedCommitter{}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, committer)

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := store.claimCalls, 1; got != want {
		t.Fatalf("claim calls = %d, want %d", got, want)
	}
	if got, want := store.completeCalls, 1; got != want {
		t.Fatalf("complete calls = %d, want %d", got, want)
	}
	if got, want := committer.claimedCalls, 1; got != want {
		t.Fatalf("claimed commit calls = %d, want %d", got, want)
	}
	if got := store.lastComplete; got.WorkItemID != item.WorkItemID || got.ClaimID != claim.ClaimID || got.FencingToken != claim.FencingToken {
		t.Fatalf("complete mutation = %#v, want item/claim/fence from claim", got)
	}
	if got := committer.lastClaimMutation; got.WorkItemID != item.WorkItemID || got.ClaimID != claim.ClaimID || got.FencingToken != claim.FencingToken {
		t.Fatalf("claimed commit mutation = %#v, want item/claim/fence from claim", got)
	}
}

func TestClaimedServiceReleasesWhenClaimHasNoGeneration(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.April, 20, 22, 10, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		release: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	service := testClaimedService(now, claim, scope.CollectorGit, store, &stubClaimedSource{}, &stubClaimedCommitter{})

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := store.releaseCalls, 1; got != want {
		t.Fatalf("release calls = %d, want %d", got, want)
	}
	if got, want := store.completeCalls, 0; got != want {
		t.Fatalf("complete calls = %d, want %d", got, want)
	}
}

func TestClaimedServiceFailsClaimWhenCommitFails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 22, 20, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	wantErr := errors.New("commit failed")
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
	}
	source := &stubClaimedSource{collected: FactsFromSlice(testScope(), testGeneration(now), testFacts(now)), ok: true}
	committer := &stubClaimedCommitter{
		claimedCommit: func(context.Context, workflow.ClaimMutation, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error {
			return wantErr
		},
	}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, committer)

	err := service.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want wrapped %v", err, wantErr)
	}
	if got, want := store.retryableFailCalls, 1; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got := store.lastRetryableFail; got.FailureClass != "commit_failure" {
		t.Fatalf("FailureClass = %q, want commit_failure", got.FailureClass)
	}
}

func TestClaimedServiceRejectsGenericCommitter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 8, 0, 0, 0, time.UTC)
	claim := testWorkflowClaim("item-claim-1", now)
	service := testClaimedService(now, claim, scope.CollectorGit, &stubClaimStore{}, &stubClaimedSource{}, &stubCommitter{})

	err := service.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
	if got, want := err.Error(), "claim-aware collector committer must implement ClaimedCommitter"; got != want {
		t.Fatalf("Run() error = %q, want %q", got, want)
	}
}

func TestClaimedServiceTerminalFailsIdentityMismatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 22, 30, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	collectedScope := testScope()
	collectedScope.ScopeID = "scope-other"
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
	}
	source := &stubClaimedSource{collected: FactsFromSlice(collectedScope, testGeneration(now), testFacts(now)), ok: true}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})

	err := service.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want identity mismatch")
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
	if got := store.lastTerminalFail; got.FailureClass != "identity_mismatch" {
		t.Fatalf("FailureClass = %q, want identity_mismatch", got.FailureClass)
	}
}

func TestClaimedServiceErrorUsesConfiguredCollectorKind(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 22, 40, 0, 0, time.UTC)
	wantErr := errors.New("claim store unavailable")
	claim := testWorkflowClaim("item-claim-1", now)
	store := &stubClaimStore{claimErr: wantErr}
	service := testClaimedService(now, claim, scope.CollectorTerraformState, store, &stubClaimedSource{}, &stubClaimedCommitter{})

	err := service.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want wrapped %v", err, wantErr)
	}
	if got := err.Error(); !strings.Contains(got, "terraform_state") || strings.Contains(got, "git work item") {
		t.Fatalf("Run() error = %q, want configured collector kind without git wording", got)
	}
}

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

func (s *stubClaimStore) FailClaimRetryable(_ context.Context, mutation workflow.ClaimMutation) error {
	s.retryableFailCalls++
	s.lastRetryableFail = mutation
	return nil
}

func (s *stubClaimStore) FailClaimTerminal(_ context.Context, mutation workflow.ClaimMutation) error {
	s.terminalFailCalls++
	s.lastTerminalFail = mutation
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
	claimedCalls      int
	lastClaimMutation workflow.ClaimMutation
	claimedCommit     func(context.Context, workflow.ClaimMutation, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error
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
