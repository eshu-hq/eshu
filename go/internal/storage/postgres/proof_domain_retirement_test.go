// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// This file is the retirement proof lane for issue #1800. It proves, end to end
// through the in-memory queue/fact harness, that stale source-local evidence is
// never returned by active-generation reads after one of the two retirement
// mechanisms fires:
//
//  1. Generation supersession. A new generation commits and the projector ack
//     promotes it to active while marking the prior active generation
//     superseded. Active-generation reads join scope.active_generation_id and
//     generation.status = 'active', so every fact in the superseded generation
//     (removed file, deleted entity, dropped dependency, replaced workload)
//     stops joining without any per-row delete.
//  2. Tombstones. Within the still-active generation, a fact emitted with
//     is_tombstone = true models collector negative evidence (a runtime/cloud
//     source fact that no longer exists). Readers that add `is_tombstone =
//     FALSE` exclude it; readers that rely on supersession alone still return
//     it, which the matrix documents as an intentional per-reader contract.
//
// The proof matrix that maps each candidate case to fact families, graph
// labels/edges, read models, and query surfaces lives in
// retirement-proof-matrix.md alongside this file.

// retirementRepoScope returns the canonical Git repository scope used by the
// supersession proofs. A single scope with two generations is enough to model a
// repo whose snapshot changed (file/entity/dependency/workload removed).
func retirementRepoScope() scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:       "scope-repo-retire",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-retire",
		Metadata: map[string]string{
			"repo_id": "repo-retire",
		},
	}
}

// retirementRepositoryFact builds one active-repository-shaped fact for a
// generation. The fact kind and source_system match the
// listActiveRepositoryFactsQuery WHERE clause so the harness models the real
// read.
func retirementRepositoryFact(scopeID, generationID, factID, digest string, observedAt time.Time) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      "repository",
		StableFactKey: "repository:" + factID,
		ObservedAt:    observedAt,
		Payload: map[string]any{
			"graph_id":   "repo-retire",
			"graph_kind": "repository",
			"name":       "retire-target",
			"digest":     digest,
		},
		SourceRef: facts.Ref{
			SourceSystem: "git",
			FactKey:      factID,
		},
	}
}

// TestProofRetirementProductionQueriesCarryRetirementPredicates ties the
// in-memory harness emulation to the real SQL. The harness emulates the active
// read by reading state directly; this test asserts the production query
// strings it stands in for actually carry the supersession join and the
// tombstone predicate, so a future SQL edit that drops a predicate is caught
// here even though the harness would otherwise keep passing.
func TestProofRetirementProductionQueriesCarryRetirementPredicates(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
	} {
		if !strings.Contains(listActiveRepositoryFactsQuery, want) {
			t.Fatalf("listActiveRepositoryFactsQuery missing supersession predicate %q", want)
		}
	}
	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.is_tombstone = FALSE",
	} {
		if !strings.Contains(listActiveContainerImageIdentityFactsQuery, want) {
			t.Fatalf("listActiveContainerImageIdentityFactsQuery missing retirement predicate %q", want)
		}
	}
}

func factIDs(envelopes []facts.Envelope) []string {
	ids := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		ids = append(ids, envelope.FactID)
	}
	return ids
}

func containsFactID(envelopes []facts.Envelope, factID string) bool {
	for _, envelope := range envelopes {
		if envelope.FactID == factID {
			return true
		}
	}
	return false
}

// TestProofRetirementSupersededGenerationFactsAreNotReturnedByActiveReads is the
// core supersession proof. It covers the candidate cases that are all realized
// as "the prior generation's fact is gone in the new generation":
//
//   - file removed from a repository
//   - entity removed or renamed
//   - dependency removed from a manifest or lockfile
//   - workload/deployment evidence removed or replaced
//
// All four collapse to: generation A carries fact-old, generation B does not.
// After B becomes active, the active-generation read must return only B's fact
// and never A's, even though A's row physically remains in fact_records.
func TestProofRetirementSupersededGenerationFactsAreNotReturnedByActiveReads(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)
	db := newProofDomainDB(t, now)
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	factStore := NewFactStore(db)
	scopeValue := retirementRepoScope()

	generationA := scope.ScopeGeneration{
		GenerationID:  "generation-a",
		ScopeID:       scopeValue.ScopeID,
		ObservedAt:    now.Add(-2 * time.Minute),
		IngestedAt:    now,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-a",
	}
	// Generation A holds the evidence that will be retired: fact-old.
	factOld := retirementRepositoryFact(scopeValue.ScopeID, generationA.GenerationID, "fact-old", "digest-a", generationA.ObservedAt)
	if err := store.CommitScopeGeneration(
		context.Background(), scopeValue, generationA, testFactChannel([]facts.Envelope{factOld}),
	); err != nil {
		t.Fatalf("CommitScopeGeneration(A) error = %v, want nil", err)
	}
	runProofProjectorCycle(t, db, now)

	if got := db.activeGenerationID(scopeValue.ScopeID); got != generationA.GenerationID {
		t.Fatalf("active generation after A = %q, want %q", got, generationA.GenerationID)
	}
	activeA, err := factStore.ListActiveRepositoryFacts(context.Background())
	if err != nil {
		t.Fatalf("ListActiveRepositoryFacts() after A error = %v, want nil", err)
	}
	if !containsFactID(activeA, "fact-old") {
		t.Fatalf("active read after A = %v, want it to contain fact-old", factIDs(activeA))
	}

	// Generation B is the refreshed snapshot that no longer carries fact-old.
	// It carries fact-new instead (file/entity/dependency/workload replaced).
	generationB := scope.ScopeGeneration{
		GenerationID:  "generation-b",
		ScopeID:       scopeValue.ScopeID,
		ObservedAt:    now.Add(-time.Minute),
		IngestedAt:    now.Add(time.Minute),
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-b",
	}
	factNew := retirementRepositoryFact(scopeValue.ScopeID, generationB.GenerationID, "fact-new", "digest-b", generationB.ObservedAt)
	if err := store.CommitScopeGeneration(
		context.Background(), scopeValue, generationB, testFactChannel([]facts.Envelope{factNew}),
	); err != nil {
		t.Fatalf("CommitScopeGeneration(B) error = %v, want nil", err)
	}
	runProofProjectorCycle(t, db, now)

	if got := db.activeGenerationID(scopeValue.ScopeID); got != generationB.GenerationID {
		t.Fatalf("active generation after B = %q, want %q", got, generationB.GenerationID)
	}
	if got := db.generationStatus(generationA.GenerationID); got != scope.GenerationStatusSuperseded {
		t.Fatalf("generation A status after B = %q, want %q", got, scope.GenerationStatusSuperseded)
	}

	// Accuracy: the superseded generation's fact must be gone from active reads,
	// the new generation's fact must be present.
	activeB, err := factStore.ListActiveRepositoryFacts(context.Background())
	if err != nil {
		t.Fatalf("ListActiveRepositoryFacts() after B error = %v, want nil", err)
	}
	if containsFactID(activeB, "fact-old") {
		t.Fatalf("active read after B = %v, want it to NOT contain superseded fact-old", factIDs(activeB))
	}
	if !containsFactID(activeB, "fact-new") {
		t.Fatalf("active read after B = %v, want it to contain fact-new", factIDs(activeB))
	}

	// The superseded row physically remains for audit history: a direct
	// per-generation read of generation A still returns fact-old. Retirement is
	// a read-time pointer move, not destructive deletion.
	historicalA, err := factStore.ListFacts(context.Background(), scopeValue.ScopeID, generationA.GenerationID)
	if err != nil {
		t.Fatalf("ListFacts(A) error = %v, want nil", err)
	}
	if !containsFactID(historicalA, "fact-old") {
		t.Fatalf("historical read of generation A = %v, want it to retain fact-old", factIDs(historicalA))
	}
}

// TestProofRetirementTombstoneInActiveGenerationIsFilteredWhenReaderOptsIn proves
// the second mechanism: a tombstone fact that survives in the active generation
// is excluded by readers that add `is_tombstone = FALSE`
// (listActiveContainerImageIdentityFacts), modeling a runtime/cloud/collector
// source fact that was observed gone. The supersession-only repository read does
// not carry that predicate; the matrix records that contrast so a reader change
// that drops the predicate is caught.
func TestProofRetirementTombstoneInActiveGenerationIsFilteredWhenReaderOptsIn(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 13, 0, 0, 0, time.UTC)
	db := newProofDomainDB(t, now)
	factStore := NewFactStore(db)

	scopeID := "scope-oci-retire"
	generationID := "generation-oci"
	db.state.scopes[scopeID] = scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  "oci_registry",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "oci-retire",
	}
	db.state.generations[generationID] = scope.ScopeGeneration{
		GenerationID: generationID,
		ScopeID:      scopeID,
		ObservedAt:   now.Add(-time.Minute),
		IngestedAt:   now,
		Status:       scope.GenerationStatusActive,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	db.state.activeGenerations[scopeID] = generationID

	// A live image-tag fact and a tombstoned image-tag fact in the SAME active
	// generation. The tombstone models a tag that the collector observed removed.
	livePayload := map[string]any{
		"registry":        "registry.example.com",
		"repository":      "team/api",
		"tag":             "prod",
		"resolved_digest": "sha256:live",
	}
	tombstonePayload := map[string]any{
		"registry":        "registry.example.com",
		"repository":      "team/api",
		"tag":             "stale",
		"resolved_digest": "sha256:stale",
	}
	db.state.facts["fact-oci-live"] = facts.Envelope{
		FactID:        "fact-oci-live",
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      "oci_registry.image_tag_observation",
		StableFactKey: "oci-tag:team-api:prod",
		ObservedAt:    now.Add(-time.Minute),
		IsTombstone:   false,
		Payload:       livePayload,
		SourceRef:     facts.Ref{SourceSystem: "oci_registry", FactKey: "oci-tag:team-api:prod"},
	}
	db.state.facts["fact-oci-tombstone"] = facts.Envelope{
		FactID:        "fact-oci-tombstone",
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      "oci_registry.image_tag_observation",
		StableFactKey: "oci-tag:team-api:stale",
		ObservedAt:    now.Add(-time.Minute),
		IsTombstone:   true,
		Payload:       tombstonePayload,
		SourceRef:     facts.Ref{SourceSystem: "oci_registry", FactKey: "oci-tag:team-api:stale"},
	}

	loaded, err := factStore.ListActiveContainerImageIdentityFacts(context.Background())
	if err != nil {
		t.Fatalf("ListActiveContainerImageIdentityFacts() error = %v, want nil", err)
	}
	if !containsFactID(loaded, "fact-oci-live") {
		t.Fatalf("active identity read = %v, want it to contain fact-oci-live", factIDs(loaded))
	}
	if containsFactID(loaded, "fact-oci-tombstone") {
		t.Fatalf("active identity read = %v, want it to exclude the tombstoned fact", factIDs(loaded))
	}
}

// TestProofRetirementEmptyGenerationIsSafe proves the empty-state edge: a scope
// with no active generation returns nothing from active reads and never errors.
// This is the "first run not yet promoted" / "all generations failed" boundary.
func TestProofRetirementEmptyGenerationIsSafe(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 14, 0, 0, 0, time.UTC)
	db := newProofDomainDB(t, now)
	factStore := NewFactStore(db)
	scopeValue := retirementRepoScope()

	// A scope and a pending (not yet active) generation with a fact, but no
	// active_generation_id pointer. Nothing should be visible to active reads.
	db.state.scopes[scopeValue.ScopeID] = scopeValue
	db.state.generations["generation-pending"] = scope.ScopeGeneration{
		GenerationID: "generation-pending",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   now.Add(-time.Minute),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	db.state.facts["fact-pending"] = retirementRepositoryFact(
		scopeValue.ScopeID, "generation-pending", "fact-pending", "digest-pending", now.Add(-time.Minute),
	)

	loaded, err := factStore.ListActiveRepositoryFacts(context.Background())
	if err != nil {
		t.Fatalf("ListActiveRepositoryFacts() empty-generation error = %v, want nil", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("active read with no active generation = %v, want empty", factIDs(loaded))
	}
}

// TestProofRetirementFirstGenerationFactsAreReturnedAndNotPrematurelyRetired
// proves the first-generation boundary on the read side: a brand-new scope whose
// only generation just became active must return its facts. Combined with the
// reducer-side first-generation retract skip (proven in
// TestIAMCanPerformHandlerSkipsFirstGenerationRetract and
// TestInfrastructurePlatformMaterializerRetractStaleEmptyRepoIDs), this shows
// the first generation is neither hidden nor subjected to a spurious retract.
func TestProofRetirementFirstGenerationFactsAreReturnedAndNotPrematurelyRetired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 9, 15, 0, 0, 0, time.UTC)
	db := newProofDomainDB(t, now)
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }
	factStore := NewFactStore(db)
	scopeValue := retirementRepoScope()

	generation := scope.ScopeGeneration{
		GenerationID:  "generation-first",
		ScopeID:       scopeValue.ScopeID,
		ObservedAt:    now.Add(-time.Minute),
		IngestedAt:    now,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-first",
	}
	factFirst := retirementRepositoryFact(scopeValue.ScopeID, generation.GenerationID, "fact-first", "digest-first", generation.ObservedAt)
	if err := store.CommitScopeGeneration(
		context.Background(), scopeValue, generation, testFactChannel([]facts.Envelope{factFirst}),
	); err != nil {
		t.Fatalf("CommitScopeGeneration(first) error = %v, want nil", err)
	}
	runProofProjectorCycle(t, db, now)

	if got := db.generationStatus(generation.GenerationID); got != scope.GenerationStatusActive {
		t.Fatalf("first generation status = %q, want %q", got, scope.GenerationStatusActive)
	}
	loaded, err := factStore.ListActiveRepositoryFacts(context.Background())
	if err != nil {
		t.Fatalf("ListActiveRepositoryFacts() first-generation error = %v, want nil", err)
	}
	if !containsFactID(loaded, "fact-first") {
		t.Fatalf("active read of first generation = %v, want it to contain fact-first", factIDs(loaded))
	}
}
