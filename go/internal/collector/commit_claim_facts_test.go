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

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
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
			ComponentID:   "git-commit-lane",
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
// never reached the plane. heartbeatMutation records the mutation the probe
// carried, so tests can pin what the lane stamped before probing.
type commitLaneStore struct {
	heartbeatErr      error
	heartbeatCalls    int
	heartbeatMutation workflow.ClaimMutation
	completeCalls     int
}

func (s *commitLaneStore) ClaimNextEligible(
	context.Context,
	workflow.ClaimSelector,
	time.Time,
	time.Duration,
) (workflow.WorkItem, workflow.Claim, bool, error) {
	return workflow.WorkItem{}, workflow.Claim{}, false, nil
}

func (s *commitLaneStore) HeartbeatClaim(_ context.Context, mutation workflow.ClaimMutation) error {
	s.heartbeatCalls++
	s.heartbeatMutation = mutation
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

// commitLaneCommitter records claimed commits. It is a plain recorder: both
// attempts of a retry reach it, because dedup lives in the production
// boundary's generation-finalized skip, which no stub can mimic.
type commitLaneCommitter struct {
	calls       int
	mutations   []workflow.ClaimMutation
	scopes      []scope.IngestionScope
	generations []scope.ScopeGeneration
	recorded    []facts.Envelope
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
	c.mutations = append(c.mutations, mutation)
	c.scopes = append(c.scopes, scopeValue)
	c.generations = append(c.generations, generation)
	for envelope := range factStream {
		c.recorded = append(c.recorded, envelope)
	}
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
		ComponentID:    "git-commit-lane",
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
	want := factenvelope.InternalFromSDKFact(commitLaneResult(mutation, 3).Facts[0], factenvelope.InternalEnvelopeOptions{
		ComponentID:   "git-commit-lane",
		ScopeID:       "scope-commit-lane",
		GenerationID:  "gen-commit-lane",
		CollectorKind: "git",
		FencingToken:  mutation.FencingToken,
	})
	if first.FactID != want.FactID {
		t.Fatalf("first envelope FactID = %q, want the canonical adapter FactID %q", first.FactID, want.FactID)
	}
}

// TestCommitClaimFactsTakesCollectorKindFromTarget proves the committed
// envelopes carry the fenced target's collector kind even when the result
// echo names another kind: reverting to echo-sourcing must turn this red.
func TestCommitClaimFactsTakesCollectorKindFromTarget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	result := commitLaneResult(mutation, 1)
	result.Claim.CollectorKind = "terraform"
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-kind",
		ResultJSON:     mustCommitLaneResultJSON(t, result),
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err != nil {
		t.Fatalf("CommitClaimFacts() error = %v, want nil", err)
	}
	if len(committer.recorded) != 1 {
		t.Fatalf("recorded envelopes = %d, want 1", len(committer.recorded))
	}
	if got := committer.recorded[0].CollectorKind; got != "git" {
		t.Fatalf("envelope CollectorKind = %q, want the fenced target %q, not the echo", got, "git")
	}
}

// TestCommitClaimFactsStaleFenceWritesNoPartialFacts proves the fenced-epoch
// half: a fenced-away epoch decodes but fails closed at the fence probe
// before anything is committed, so a stale retry leaves zero partial facts.
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
		ComponentID:    "git-commit-lane",
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

// TestCommitClaimFactsStampsFreshObservedAtForProbe pins the lease math the
// Postgres heartbeat runs: lease_expires_at derives from the probed
// ObservedAt, so a claim-time ObservedAt from a satellite that collected for
// minutes would write an already-past expiry. The lane stamps now (UTC)
// immediately before the probe, so even a stale caller-supplied ObservedAt
// succeeds with a live lease. Deleting the stamp turns this test red.
func TestCommitClaimFactsStampsFreshObservedAtForProbe(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-30 * time.Minute)
	mutation := commitLaneMutation(stale)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(stale),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-stale-observed",
		ResultJSON:     mustCommitLaneResultJSON(t, commitLaneResult(mutation, 1)),
	}
	before := time.Now().UTC()
	if err := CommitClaimFacts(context.Background(), store, committer, req); err != nil {
		t.Fatalf("CommitClaimFacts() error = %v, want nil for a stale caller ObservedAt", err)
	}
	if store.heartbeatCalls != 1 || committer.calls != 1 {
		t.Fatalf("probe calls = %d, commit calls = %d, want 1/1 for a stamped probe",
			store.heartbeatCalls, committer.calls)
	}
	got := store.heartbeatMutation.ObservedAt
	if !got.After(stale) {
		t.Fatalf("probe ObservedAt = %v, want after the stale caller value %v", got, stale)
	}
	if got.Before(before.Add(-time.Minute)) {
		t.Fatalf("probe ObservedAt = %v, want stamped at test time, not carried stale", got)
	}
	if got.After(time.Now().UTC().Add(time.Minute)) {
		t.Fatalf("probe ObservedAt = %v, want no later than now", got)
	}
	if !committer.mutations[0].ObservedAt.Equal(got) {
		t.Fatal("commit mutation ObservedAt does not match the stamped probe value")
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
		ComponentID:    "git-commit-lane",
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
		ComponentID:    "git-commit-lane",
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
		ComponentID:    "git-commit-lane",
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
// any plane contact: the key travels for wire symmetry with the claim-plane
// protocol, even though the generation (not the key) is the retry key the
// boundary dedupes on.
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
		// ComponentID is set so the missing key is the only failure:
		// deleting the key check must turn this test red.
		ComponentID: "git-commit-lane",
		ResultJSON:  mustCommitLaneResultJSON(t, commitLaneResult(mutation, 1)),
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want missing idempotency key")
	}
	if store.heartbeatCalls != 0 || committer.calls != 0 {
		t.Fatalf("store calls = %d, commit calls = %d, want 0/0 for a keyless request",
			store.heartbeatCalls, committer.calls)
	}
}
