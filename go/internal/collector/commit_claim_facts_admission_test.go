// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestCommitClaimFactsRejectsUnknownProtocolVersion pins the version
// allowlist: only the non-paged SDK versions commit through the single-call
// lane. An unknown version fails closed before the fence probe.
func TestCommitClaimFactsRejectsUnknownProtocolVersion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	result := commitLaneResult(mutation, 1)
	result.ProtocolVersion = "collector-sdk/v9"
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-bad-version",
		ResultJSON:     mustCommitLaneResultJSON(t, result),
	}
	err := CommitClaimFacts(context.Background(), store, committer, req)
	if err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want protocol-version rejection")
	}
	if !strings.Contains(err.Error(), "protocol_version") {
		t.Fatalf("CommitClaimFacts() error = %v, want it to name protocol_version", err)
	}
	if store.heartbeatCalls != 0 {
		t.Fatalf("fence probe calls = %d, want 0: version rejects before the probe", store.heartbeatCalls)
	}
	if committer.calls != 0 {
		t.Fatalf("commit calls = %d, want 0 for an unknown version", committer.calls)
	}
}

// TestCommitClaimFactsRejectsPagedProtocolVersion pins that the single-call
// lane never assembles pages: v1alpha3 always carries a page envelope and
// this lane cannot fetch the rest, so it fails closed terminally.
func TestCommitClaimFactsRejectsPagedProtocolVersion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	result := commitLaneResult(mutation, 1)
	result.ProtocolVersion = "collector-sdk/v1alpha3"
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-paged-version",
		ResultJSON:     mustCommitLaneResultJSON(t, result),
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want paged-version rejection")
	}
	if committer.calls != 0 {
		t.Fatalf("commit calls = %d, want 0 for a paged version", committer.calls)
	}
}

// TestCommitClaimFactsRejectsPageEnvelope pins the explicit page verdict: a
// result carrying a page continuation must never commit one page as the whole
// generation, even when its version is allowlisted.
func TestCommitClaimFactsRejectsPageEnvelope(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	raw := string(mustCommitLaneResultJSON(t, commitLaneResult(mutation, 1)))
	paged, ok := injectPageEnvelope(raw)
	if !ok {
		t.Fatal("test fixture lacks a facts array to attach the page envelope to")
	}
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-page",
		ResultJSON:     []byte(paged),
	}
	err := CommitClaimFacts(context.Background(), store, committer, req)
	if err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want page-envelope rejection")
	}
	if !strings.Contains(err.Error(), "page envelope") {
		t.Fatalf("CommitClaimFacts() error = %v, want the explicit page-envelope verdict", err)
	}
	if committer.calls != 0 {
		t.Fatalf("commit calls = %d, want 0 for a paged result", committer.calls)
	}
}

// injectPageEnvelope attaches a v1alpha3 continuation envelope to a marshaled
// result, keeping its allowlisted version: only the envelope is foreign.
func injectPageEnvelope(raw string) (string, bool) {
	at := strings.Index(raw, `"facts":`)
	if at < 0 {
		return "", false
	}
	envelope := `"page":{"cursor":"page-2","more":true,"page_index":0},`
	return raw[:at] + envelope + raw[at:], true
}

// TestCommitClaimFactsRejectsStatuses pins the explicit status disposition:
// the lane records no statuses, so a status-bearing result fails closed
// instead of committing facts while its signals are silently dropped.
func TestCommitClaimFactsRejectsStatuses(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	result := commitLaneResult(mutation, 1)
	result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusWarning}}
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-statuses",
		ResultJSON:     mustCommitLaneResultJSON(t, result),
	}
	err := CommitClaimFacts(context.Background(), store, committer, req)
	if err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want status disposition rejection")
	}
	if !strings.Contains(err.Error(), "status") {
		t.Fatalf("CommitClaimFacts() error = %v, want it to name statuses", err)
	}
	if committer.calls != 0 {
		t.Fatalf("commit calls = %d, want 0 for a status-bearing result", committer.calls)
	}
}

// TestCommitClaimFactsRejectsUndecodableWithoutProbe pins the order: shape
// and decode run before any plane contact, so an undecodable carrier never
// extends a dead lease.
func TestCommitClaimFactsRejectsUndecodableWithoutProbe(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-garbage",
		ResultJSON:     []byte(`{"state":"comple`),
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want decode rejection")
	}
	if store.heartbeatCalls != 0 {
		t.Fatalf("fence probe calls = %d, want 0: garbage must not extend the lease", store.heartbeatCalls)
	}
	if committer.calls != 0 {
		t.Fatalf("commit calls = %d, want 0 for garbage bytes", committer.calls)
	}
}

// TestCommitClaimFactsRequiresComponentID pins that the caller-owned
// component identity is mandatory: without it the lane cannot seed envelope
// FactIDs or reject a cross-component carrier.
func TestCommitClaimFactsRequiresComponentID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		IdempotencyKey: "commit-lane-key-no-component",
		ResultJSON:     mustCommitLaneResultJSON(t, commitLaneResult(mutation, 1)),
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want missing component id")
	}
	if store.heartbeatCalls != 0 || committer.calls != 0 {
		t.Fatalf("store calls = %d, commit calls = %d, want 0/0 for a componentless request",
			store.heartbeatCalls, committer.calls)
	}
}

// TestCommitClaimFactsRejectsComponentMismatch fails closed when the result
// echo names a different component than the caller: FactIDs seed from the
// caller-owned identity, so a cross-component carrier never commits.
func TestCommitClaimFactsRejectsComponentMismatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	result := commitLaneResult(mutation, 1)
	result.Claim.ComponentID = "someone-else"
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-component-mismatch",
		ResultJSON:     mustCommitLaneResultJSON(t, result),
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want component mismatch")
	}
	if committer.calls != 0 {
		t.Fatalf("commit calls = %d, want 0 for a cross-component carrier", committer.calls)
	}
}

// TestCommitClaimFactsPreservesPayloadPrecision pins the canonical decode
// discipline: a payload integer past float64 range survives the lane exactly.
// Without UseNumber it degrades to the nearest float64 and this fails.
func TestCommitClaimFactsPreservesPayloadPrecision(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	raw := string(mustCommitLaneResultJSON(t, commitLaneResult(mutation, 1)))
	precise := strings.Replace(raw, `"index":0`, `"bign":9007199254740993`, 1)
	if precise == raw {
		t.Fatal("test fixture lacks the small-int payload to widen")
	}
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-precision",
		ResultJSON:     []byte(precise),
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err != nil {
		t.Fatalf("CommitClaimFacts() error = %v, want nil", err)
	}
	got, ok := committer.recorded[0].Payload["bign"].(json.Number)
	if !ok {
		t.Fatalf("payload bign type = %T, want json.Number", committer.recorded[0].Payload["bign"])
	}
	if got.String() != "9007199254740993" {
		t.Fatalf("payload bign = %q, want exact 9007199254740993", got.String())
	}
}

// TestCommitClaimFactsDedupesIdenticalFacts pins parity with the in-tree
// mapper: same (kind, stableKey) twice collapses to one envelope.
func TestCommitClaimFactsDedupesIdenticalFacts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	result := commitLaneResult(mutation, 1)
	result.Facts = append(result.Facts, result.Facts[0])
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-dedupe",
		ResultJSON:     mustCommitLaneResultJSON(t, result),
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err != nil {
		t.Fatalf("CommitClaimFacts() error = %v, want nil", err)
	}
	if len(committer.recorded) != 1 {
		t.Fatalf("recorded envelopes = %d, want 1: identical facts collapse", len(committer.recorded))
	}
}

// TestCommitClaimFactsForwardsTrustedDeletion pins that the result's deletion
// attestation reaches the committer's generation: without it the store's
// successor-emptiness backstop keeps a legitimately emptied scope pinned.
func TestCommitClaimFactsForwardsTrustedDeletion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{}
	committer := &commitLaneCommitter{}

	result := commitLaneResult(mutation, 0)
	result.Generation.TrustedDeletion = true
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-deletion",
		ResultJSON:     mustCommitLaneResultJSON(t, result),
	}
	if err := CommitClaimFacts(context.Background(), store, committer, req); err != nil {
		t.Fatalf("CommitClaimFacts() error = %v, want nil", err)
	}
	if !committer.generations[0].TrustedDeletion {
		t.Fatal("committed generation TrustedDeletion = false, want the result attestation forwarded")
	}
}

// TestCommitClaimFactsStaleFenceSkipsProbeAfterDecode keeps the stale-fence
// half green under the new order: a fenced-away epoch still writes nothing,
// decoded or not.
func TestCommitClaimFactsStaleFenceSkipsProbeAfterDecode(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	mutation := commitLaneMutation(now)
	store := &commitLaneStore{heartbeatErr: workflow.ErrClaimRejected}
	committer := &commitLaneCommitter{}

	resultJSON := mustCommitLaneResultJSON(t, commitLaneResult(mutation, 1))
	req := CommitClaimFactsRequest{
		Mutation:       mutation,
		Scope:          commitLaneScope(),
		Generation:     commitLaneGeneration(now),
		ComponentID:    "git-commit-lane",
		IdempotencyKey: "commit-lane-key-stale-order",
		ResultJSON:     resultJSON,
	}
	err := CommitClaimFacts(context.Background(), store, committer, req)
	if err == nil {
		t.Fatal("CommitClaimFacts() error = nil, want fence rejection")
	}
	if store.heartbeatCalls != 1 {
		t.Fatalf("fence probe calls = %d, want 1: a decodable carrier still probes", store.heartbeatCalls)
	}
	if committer.calls != 0 {
		t.Fatalf("commit calls = %d, want 0 on a stale fence", committer.calls)
	}
}
