// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// commitLaneMutation returns one fenced epoch: the planned identity the
// #9376 pin carries plus a live fencing token and lease.
func commitLaneMutation(now time.Time) workflow.ClaimMutation {
	return workflow.ClaimMutation{
		WorkItemID:    "work-item-commit-lane",
		ClaimID:       "claim-commit-lane",
		FencingToken:  7,
		OwnerID:       "owner-commit-lane",
		ObservedAt:    now,
		LeaseDuration: time.Minute,
		ScopeID:       "scope-commit-lane",
		SourceRunID:   "source-run-commit-lane",
		GenerationID:  "gen-commit-lane",
	}
}

func commitLaneScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       "scope-commit-lane",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-commit-lane",
	}
}

func commitLaneGeneration(now time.Time) scope.ScopeGeneration {
	return scope.ScopeGeneration{
		GenerationID: "gen-commit-lane",
		ScopeID:      "scope-commit-lane",
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
}

// commitLaneResult builds one complete SDK result echoing the mutation epoch:
// every fact carries the fenced target in its source ref, the way the
// sbom-shadow child derives refs from the claim it ran under.
func commitLaneResult(mutation workflow.ClaimMutation, factCount int) sdkcollector.Result {
	resultFacts := make([]sdkcollector.Fact, 0, factCount)
	for i := 0; i < factCount; i++ {
		key := strings.Repeat("k", i+1)
		resultFacts = append(resultFacts, sdkcollector.Fact{
			Kind:             "SBOMComponentFact",
			SchemaVersion:    "sbom/v1",
			StableKey:        "stable-" + key,
			SourceConfidence: sdkcollector.SourceConfidenceObserved,
			ObservedAt:       mutation.ObservedAt,
			SourceRef: sdkcollector.SourceRef{
				SourceSystem: "git",
				ScopeID:      "scope-commit-lane",
				GenerationID: "gen-commit-lane",
				FactKey:      "stable-" + key,
				URI:          "https://eshu.invalid/pinned/sbom.json",
				RecordID:     "record-" + key,
			},
			Payload: map[string]any{"index": i},
		})
	}
	return sdkcollector.Result{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		State:           sdkcollector.ResultComplete,
		Claim: sdkcollector.Claim{
			CollectorKind: "git",
			InstanceID:    "git-commit-lane",
			SourceSystem:  "git",
			SourceRunID:   "source-run-commit-lane",
			GenerationID:  "gen-commit-lane",
			WorkItemID:    mutation.WorkItemID,
			FencingToken:  "7",
		},
		Generation: sdkcollector.Generation{ID: "gen-commit-lane", ObservedAt: mutation.ObservedAt},
		Facts:      resultFacts,
	}
}

func mustCommitLaneResultJSON(t *testing.T, result sdkcollector.Result) []byte {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	return raw
}

// commitLaneStore is a ClaimControlStore stub: the fence probe answers with
// heartbeatErr, CompleteClaim acks and records. Zero calls means the lane
// never reached the plane.
type commitLaneStore struct {
	heartbeatErr   error
	heartbeatCalls int
	completeCalls  int
}

func (s *commitLaneStore) ClaimNextEligible(
	context.Context,
	workflow.ClaimSelector,
	time.Time,
	time.Duration,
) (workflow.WorkItem, workflow.Claim, bool, error) {
	return workflow.WorkItem{}, workflow.Claim{}, false, nil
}

func (s *commitLaneStore) HeartbeatClaim(_ context.Context, _ workflow.ClaimMutation) error {
	s.heartbeatCalls++
	return s.heartbeatErr
}

func (s *commitLaneStore) CompleteClaim(_ context.Context, _ workflow.ClaimMutation) error {
	s.completeCalls++
	return nil
}

func (s *commitLaneStore) ReleaseClaim(context.Context, workflow.ClaimMutation) error {
	return nil
}

func (s *commitLaneStore) FailClaimRetryable(context.Context, workflow.ClaimMutation) error {
	return nil
}

func (s *commitLaneStore) FailClaimTerminal(context.Context, workflow.ClaimMutation) error {
	return nil
}

// commitLaneCommitter records claimed commits. When mimicFinalizedSkip is
// set, a second commit of an already-committed generation drains the stream
// and succeeds without re-recording, mirroring the real boundary's
// generation-finalized skip that makes retries idempotent.
type commitLaneCommitter struct {
	calls                int
	mutations            []workflow.ClaimMutation
	scopes               []scope.IngestionScope
	generations          []scope.ScopeGeneration
	recorded             []facts.Envelope
	committedGenerations map[string]bool
	mimicFinalizedSkip   bool
}

func (c *commitLaneCommitter) CommitScopeGeneration(
	context.Context,
	scope.IngestionScope,
	scope.ScopeGeneration,
	<-chan facts.Envelope,
) error {
	return errors.New("generic commit should not be used")
}

func (c *commitLaneCommitter) CommitClaimedScopeGeneration(
	_ context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	c.calls++
	if c.mimicFinalizedSkip && c.committedGenerations[generation.GenerationID] {
		for range factStream {
		}
		return nil
	}
	if c.committedGenerations == nil {
		c.committedGenerations = map[string]bool{}
	}
	c.mutations = append(c.mutations, mutation)
	c.scopes = append(c.scopes, scopeValue)
	c.generations = append(c.generations, generation)
	for envelope := range factStream {
		c.recorded = append(c.recorded, envelope)
	}
	c.committedGenerations[generation.GenerationID] = true
	return nil
}

// TestCommitClaimFactsRoundTripsResultBytes is the commit-lane contract: a
// Complete-with-result carries fact bytes through the fenced epoch into the
// collector-owned commit boundary. On the pre-lane tree the only completion
// path acks and drops the bytes, so this fails with zero recorded envelopes.
func TestCommitClaimFactsRoundTripsResultBytes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}
	resultJSON := mustCommitLaneResultJSON(t, commitLaneResult(mutation, 3))

	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		IdempotencyKey: "commit-lane-key-1",
		ResultJSON:     resultJSON,
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err != nil {
		t.Fatalf("CommitClaimFacts() error = %v, want nil", err)
	}
	if store.heartbeatCalls != 1 {
		t.Fatalf("fence probe calls = %d, want 1", store.heartbeatCalls)
	}
	if committer.calls != 1 {
		t.Fatalf("commit calls = %d, want 1", committer.calls)
	}
	if len(committer.recorded) != 3 {
		t.Fatalf("recorded envelopes = %d, want 3", len(committer.recorded))
	}
	first := committer.recorded[0]
	if first.FactKind != "SBOMComponentFact" || first.StableFactKey != "stable-k" {
		t.Fatalf("first envelope = %+v, want SBOMComponentFact/stable-k", first)
	}
	if first.ScopeID != "scope-commit-lane" || first.GenerationID != "gen-commit-lane" {
		t.Fatalf("first envelope target = %q/%q, want scope-commit-lane/gen-commit-lane",
			first.ScopeID, first.GenerationID)
	}
	if first.FencingToken != mutation.FencingToken {
		t.Fatalf("first envelope fencing token = %d, want %d", first.FencingToken, mutation.FencingToken)
	}
	if first.SourceRef.SourceURI != "https://eshu.invalid/pinned/sbom.json" {
		t.Fatalf("first envelope source URI = %q, want pinned document", first.SourceRef.SourceURI)
	}
	if got := committer.mutations[0]; got.ClaimID != mutation.ClaimID || got.FencingToken != mutation.FencingToken {
		t.Fatalf("commit mutation = %+v, want the fenced epoch", got)
	}
	if store.completeCalls != 0 {
		t.Fatalf("CompleteClaim calls = %d, want 0: commit and complete stay separate", store.completeCalls)
	}
}

// TestCommitClaimFactsStaleFenceWritesNoPartialFacts proves the fenced-epoch
// half: a fenced-away epoch fails closed before any byte is decoded or
// committed, so a stale retry leaves zero partial facts.
func TestCommitClaimFactsStaleFenceWritesNoPartialFacts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{heartbeatErr: workflow.ErrClaimRejected}
	committer := &commitLaneCommitter{}
	resultJSON := mustCommitLaneResultJSON(t, commitLaneResult(mutation, 3))

	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		IdempotencyKey: "commit-lane-key-stale",
		ResultJSON:     resultJSON,
	}
	err := CommitClaimFacts(context.Background(), store, committer, req)
	if err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want fence rejection")
	}
	if !errors.Is(err, workflow.ErrClaimRejected) {
		t.Fatalf("CommitClaimFacts() error = %v, want ErrClaimRejected", err)
	}
	if committer.calls != 0 {
		t.Fatalf("commit calls = %d, want 0: stale fence must write no partial facts", committer.calls)
	}
}

// TestCommitClaimFactsRetryIsIdempotent proves the retry half: repeating the
// same idempotency key after a committed generation succeeds without
// duplicating facts, so at-least-once redelivery is safe.
func TestCommitClaimFactsRetryIsIdempotent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{mimicFinalizedSkip: true}
	resultJSON := mustCommitLaneResultJSON(t, commitLaneResult(mutation, 2))

	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		IdempotencyKey: "commit-lane-key-retry",
		ResultJSON:     resultJSON,
	}
	ctx := context.Background()
	if err := CommitClaimFacts(ctx, store, committer, req); err != nil {
		t.Fatalf("first CommitClaimFacts() error = %v, want nil", err)
	}
	if err := CommitClaimFacts(ctx, store, committer, req); err != nil {
		t.Fatalf("retry CommitClaimFacts() error = %v, want nil", err)
	}
	if len(committer.recorded) != 2 {
		t.Fatalf("recorded envelopes after retry = %d, want 2 (no duplicates)", len(committer.recorded))
	}
}

// TestCommitClaimFactsRejectsOversizedPayload mirrors the oversized-metadata
// fail-closed contract: a payload past the byte cap fails before any fence
// probe or commit, so an unbounded satellite cannot exhaust the committer.
func TestCommitClaimFactsRejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	// Invalid JSON by design: the size gate runs before decode, so even
	// undecodable bytes past the cap must trip the size error first.
	oversized := make([]byte, workflow.MaxCommitClaimFactsBytes+1)
	for i := range oversized {
		oversized[i] = 'x'
	}
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		IdempotencyKey: "commit-lane-key-oversized",
		ResultJSON:     oversized,
	}
	err := CommitClaimFacts(context.Background(), store, committer, req)
	if !errors.Is(err, ErrCommitClaimFactsTooLarge) {
		t.Fatalf("CommitClaimFacts() error = %v, want ErrCommitClaimFactsTooLarge", err)
	}
	if store.heartbeatCalls != 0 {
		t.Fatalf("fence probe calls = %d, want 0 for an oversized payload", store.heartbeatCalls)
	}
	if committer.calls != 0 {
		t.Fatalf("commit calls = %d, want 0 for an oversized payload", committer.calls)
	}
}

// TestCommitClaimFactsRejectsUnknownFields pins strict decoding: an unknown
// field — the exact shape the plane drops today when result_json arrives on
// Complete — fails closed instead of being silently consumed.
func TestCommitClaimFactsRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	result := commitLaneResult(mutation, 1)
	raw := mustCommitLaneResultJSON(t, result)
	var shaped map[string]any
	if err := json.Unmarshal(raw, &shaped); err != nil {
		t.Fatalf("reshape result: %v", err)
	}
	shaped["result_json_extra"] = "dropped-on-the-floor"
	tainted, err := json.Marshal(shaped)
	if err != nil {
		t.Fatalf("marshal tainted result: %v", err)
	}
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		IdempotencyKey: "commit-lane-key-tainted",
		ResultJSON:     tainted,
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want strict-decode rejection")
	}
	if committer.calls != 0 {
		t.Fatalf("commit calls = %d, want 0 for unknown fields", committer.calls)
	}
}

// TestCommitClaimFactsRejectsIdentityMismatch fails closed when the result
// echoes a different generation than the fenced target: facts for another
// generation must never land in this epoch.
func TestCommitClaimFactsRejectsIdentityMismatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	result := commitLaneResult(mutation, 1)
	result.Generation.ID = "gen-someone-else"
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		IdempotencyKey: "commit-lane-key-mismatch",
		ResultJSON:     mustCommitLaneResultJSON(t, result),
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want identity mismatch")
	}
	if committer.calls != 0 {
		t.Fatalf("commit calls = %d, want 0 for an identity mismatch", committer.calls)
	}
}

// TestCommitClaimFactsRequiresIdempotencyKey fails a keyless commit before
// any plane contact: without the key a retry cannot prove itself a replay.
func TestCommitClaimFactsRequiresIdempotencyKey(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	req := CommitClaimFactsRequest{
		Mutation:   mutation,
		Scope:      commitLaneScope(),
		Generation: commitLaneGeneration(now),
		ResultJSON: mustCommitLaneResultJSON(t, commitLaneResult(mutation, 1)),
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want missing idempotency key")
	}
	if store.heartbeatCalls != 0 || committer.calls != 0 {
		t.Fatalf("store calls = %d, commit calls = %d, want 0/0 for a keyless request",
			store.heartbeatCalls, committer.calls)
	}
}

// TestCompleteClaimStaysStateOnly guards the lane's other half: completing a
// claim never touches the commit boundary, so facts flow only through the
// fenced commit op.
func TestCompleteClaimStaysStateOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	if err := store.CompleteClaim(context.Background(), commitLaneMutation(now)); err != nil {
		t.Fatalf("CompleteClaim() error = %v, want nil", err)
	}
	if committer.calls != 0 {
		t.Fatalf("commit calls = %d, want 0: Complete stays state-only", committer.calls)
	}
}
