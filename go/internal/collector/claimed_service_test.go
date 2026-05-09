package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
	committer := &stubCommitter{}
	service := ClaimedService{
		ControlStore:        store,
		Source:              source,
		Committer:           committer,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-primary",
		OwnerID:             "collector-owner-1",
		ClaimIDFunc:         func() string { return claim.ClaimID },
		PollInterval:        time.Millisecond,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   time.Millisecond,
		Clock:               func() time.Time { return now },
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := store.claimCalls, 1; got != want {
		t.Fatalf("claim calls = %d, want %d", got, want)
	}
	if got, want := store.completeCalls, 1; got != want {
		t.Fatalf("complete calls = %d, want %d", got, want)
	}
	if got, want := committer.calls, 1; got != want {
		t.Fatalf("commit calls = %d, want %d", got, want)
	}
	if got := store.lastComplete; got.WorkItemID != item.WorkItemID || got.ClaimID != claim.ClaimID || got.FencingToken != claim.FencingToken {
		t.Fatalf("complete mutation = %#v, want item/claim/fence from claim", got)
	}
}

func TestClaimedServiceUsesFenceAwareCommitterWhenAvailable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
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
	committer := &stubClaimedCommitter{}
	service := ClaimedService{
		ControlStore:        store,
		Source:              &stubClaimedSource{collected: FactsFromSlice(testScope(), testGeneration(now), testFacts(now)), ok: true},
		Committer:           committer,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-primary",
		OwnerID:             "collector-owner-1",
		ClaimIDFunc:         func() string { return claim.ClaimID },
		PollInterval:        time.Millisecond,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   time.Millisecond,
		Clock:               func() time.Time { return now },
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := committer.claimedCalls, 1; got != want {
		t.Fatalf("claimed commit calls = %d, want %d", got, want)
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
	service := ClaimedService{
		ControlStore:        store,
		Source:              &stubClaimedSource{},
		Committer:           &stubCommitter{},
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-primary",
		OwnerID:             "collector-owner-1",
		ClaimIDFunc:         func() string { return claim.ClaimID },
		PollInterval:        time.Millisecond,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   time.Second,
		Clock:               func() time.Time { return now },
	}

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
	service := ClaimedService{
		ControlStore: store,
		Source:       &stubClaimedSource{collected: FactsFromSlice(testScope(), testGeneration(now), testFacts(now)), ok: true},
		Committer: &stubCommitter{
			commit: func(context.Context, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error {
				return wantErr
			},
		},
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-primary",
		OwnerID:             "collector-owner-1",
		ClaimIDFunc:         func() string { return claim.ClaimID },
		PollInterval:        time.Millisecond,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   time.Second,
		Clock:               func() time.Time { return now },
	}

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
	service := ClaimedService{
		ControlStore:        store,
		Source:              &stubClaimedSource{collected: FactsFromSlice(collectedScope, testGeneration(now), testFacts(now)), ok: true},
		Committer:           &stubCommitter{},
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-primary",
		OwnerID:             "collector-owner-1",
		ClaimIDFunc:         func() string { return claim.ClaimID },
		PollInterval:        time.Millisecond,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   time.Second,
		Clock:               func() time.Time { return now },
	}

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

	wantErr := errors.New("claim store unavailable")
	service := ClaimedService{
		ControlStore:        &stubClaimStore{claimErr: wantErr},
		Source:              &stubClaimedSource{},
		Committer:           &stubCommitter{},
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		OwnerID:             "collector-owner-1",
		ClaimIDFunc:         func() string { return "claim-tfstate-1" },
		PollInterval:        time.Millisecond,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   time.Second,
		Clock:               func() time.Time { return time.Date(2026, time.April, 20, 22, 40, 0, 0, time.UTC) },
	}

	err := service.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want wrapped %v", err, wantErr)
	}
	if got := err.Error(); !strings.Contains(got, "terraform_state") || strings.Contains(got, "git work item") {
		t.Fatalf("Run() error = %q, want configured collector kind without git wording", got)
	}
}

func TestClaimedServiceUsesClassifiedCollectFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 20, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.CollectorKind = scope.CollectorTerraformState
	item.SourceSystem = string(scope.CollectorTerraformState)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
	}
	service := ClaimedService{
		ControlStore:        store,
		Source:              &stubClaimedSource{err: terraformstate.WaitingOnGitGenerationError{RepoIDs: []string{"platform-infra"}}},
		Committer:           &stubCommitter{},
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		OwnerID:             "collector-owner-1",
		ClaimIDFunc:         func() string { return claim.ClaimID },
		PollInterval:        time.Millisecond,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   time.Second,
		Clock:               func() time.Time { return now },
	}

	err := service.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want waiting error")
	}
	if got, want := store.lastRetryableFail.FailureClass, "waiting_on_git_generation"; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
}

func TestClaimedServiceRecordsTerraformStateClaimWait(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 20, 10, 0, 0, time.UTC)
	item := testClaimedWorkItem(now.Add(-10 * time.Second))
	item.CollectorKind = scope.CollectorTerraformState
	item.SourceSystem = string(scope.CollectorTerraformState)
	item.ScopeID = "scope-claim-1"
	item.GenerationID = "generation-claim-1"
	item.SourceRunID = item.GenerationID
	item.VisibleAt = now.Add(-5 * time.Second)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		release: func(context.Context, workflow.ClaimMutation) error {
			return nil
		},
	}
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("claimed-service-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	service := ClaimedService{
		ControlStore:        store,
		Source:              &stubClaimedSource{},
		Committer:           &stubCommitter{},
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		OwnerID:             "collector-owner-1",
		ClaimIDFunc:         func() string { return claim.ClaimID },
		PollInterval:        time.Millisecond,
		ClaimLeaseTTL:       time.Minute,
		HeartbeatInterval:   time.Second,
		Clock:               func() time.Time { return now },
		Instruments:         instruments,
	}

	ctx, cancel := context.WithCancel(context.Background())
	store.release = func(context.Context, workflow.ClaimMutation) error {
		cancel()
		return nil
	}
	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := claimedHistogramCount(t, rm, "eshu_dp_tfstate_claim_wait_seconds"); got != 1 {
		t.Fatalf("eshu_dp_tfstate_claim_wait_seconds count = %d, want 1", got)
	}
	if claimedHistogramHasAttr(t, rm, "eshu_dp_tfstate_claim_wait_seconds", "scope_id") {
		t.Fatal("eshu_dp_tfstate_claim_wait_seconds has scope_id label, want bounded labels only")
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

func claimedHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, metricName string) uint64 {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", metricName, metricRecord.Data)
			}
			var count uint64
			for _, point := range histogram.DataPoints {
				count += point.Count
			}
			return count
		}
	}
	t.Fatalf("metric %s not found", metricName)
	return 0
}

func claimedHistogramHasAttr(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	key string,
) bool {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", metricName, metricRecord.Data)
			}
			for _, point := range histogram.DataPoints {
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == key {
						return true
					}
				}
			}
			return false
		}
	}
	t.Fatalf("metric %s not found", metricName)
	return false
}
