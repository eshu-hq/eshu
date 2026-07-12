// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestListScopeGenerationWorkEnumeratesCorpus proves the re-drain enumeration
// used by the B-12 determinism composition (issue #5008): every distinct
// (scope_id, generation_id) present in fact_records is returned as a fully
// hydrated projector.ScopeGenerationWork so concurrentreplay.FactSliceSource can
// re-drain the corpus into a fresh graph DB. The rows are the exact 16-column
// projection scanProjectorWork consumes, so the enumeration reuses that scanner.
func TestListScopeGenerationWorkEnumeratesCorpus(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, time.April, 12, 8, 0, 0, 0, time.UTC)
	ingested := time.Date(2026, time.April, 12, 8, 5, 0, 0, time.UTC)

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{
			enumWorkRow("scope-a", "git", "repository", "collector-git", "gen-a1", observed, ingested),
			enumWorkRow("scope-b", "github", "repository", "collector-github", "gen-b1", observed, ingested),
		}}},
	}
	store := NewFactStore(db)

	works, err := store.ListScopeGenerationWork(context.Background())
	if err != nil {
		t.Fatalf("ListScopeGenerationWork() error = %v, want nil", err)
	}

	if got, want := len(works), 2; got != want {
		t.Fatalf("work count = %d, want %d", got, want)
	}

	// Full hydration: FactSliceSource.Next feeds the whole Scope/Generation into
	// collector.FactsFromSlice, so a partial (IDs-only) hydration would degrade
	// the re-drained graph. Assert the metadata fields round-trip.
	first := works[0]
	if first.Scope.ScopeID != "scope-a" || first.Generation.GenerationID != "gen-a1" {
		t.Fatalf("work[0] scope/gen = %q/%q, want scope-a/gen-a1", first.Scope.ScopeID, first.Generation.GenerationID)
	}
	if first.Scope.SourceSystem != "git" {
		t.Fatalf("work[0] source_system = %q, want git", first.Scope.SourceSystem)
	}
	if first.Scope.ScopeKind != scope.ScopeKind("repository") {
		t.Fatalf("work[0] scope_kind = %q, want repository", first.Scope.ScopeKind)
	}
	if first.Scope.CollectorKind != scope.CollectorKind("collector-git") {
		t.Fatalf("work[0] collector_kind = %q, want collector-git", first.Scope.CollectorKind)
	}
	if !first.Generation.ObservedAt.Equal(observed) {
		t.Fatalf("work[0] observed_at = %v, want %v", first.Generation.ObservedAt, observed)
	}
	if first.Generation.ScopeID != "scope-a" {
		t.Fatalf("work[0] generation.scope_id = %q, want scope-a (back-filled from scope)", first.Generation.ScopeID)
	}

	if works[1].Scope.ScopeID != "scope-b" || works[1].Generation.GenerationID != "gen-b1" {
		t.Fatalf("work[1] scope/gen = %q/%q, want scope-b/gen-b1", works[1].Scope.ScopeID, works[1].Generation.GenerationID)
	}

	// Query-shape contract: enumerate distinctly from fact_records, hydrate via the
	// scope/generation joins, and order deterministically so N=1 and N=4 re-drain
	// the identical work sequence.
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(db.queries))
	}
	q := db.queries[0].query
	for _, want := range []string{"fact_records", "DISTINCT", "ingestion_scopes", "scope_generations", "ORDER BY"} {
		if !strings.Contains(q, want) {
			t.Fatalf("enumeration query missing %q:\n%s", want, q)
		}
	}
}

// TestListScopeGenerationWorkNilDB guards the same missing-database contract the
// sibling loaders enforce.
func TestListScopeGenerationWorkNilDB(t *testing.T) {
	t.Parallel()

	store := NewFactStore(nil)
	if _, err := store.ListScopeGenerationWork(context.Background()); err == nil {
		t.Fatal("ListScopeGenerationWork() with nil db error = nil, want non-nil")
	}
}

// enumWorkRow builds the 16-column scanProjectorWork projection row for the
// enumeration query (attempt_count is a literal 0 for a re-drain; no prior
// generation exists for a single-generation B-12 fixture).
func enumWorkRow(
	scopeID, sourceSystem, scopeKind, collectorKind, generationID string,
	observed, ingested time.Time,
) []any {
	return []any{
		scopeID,       // scope_id
		sourceSystem,  // source_system
		scopeKind,     // scope_kind
		"",            // parent_scope_id
		generationID,  // active_generation_id
		false,         // previous_generation_exists
		collectorKind, // collector_kind
		"",            // partition_key
		generationID,  // generation_id
		0,             // attempt_count
		observed,      // observed_at
		ingested,      // ingested_at
		"accepted",    // status
		"scheduled",   // trigger_kind
		"",            // freshness_hint
		[]byte(`{}`),  // payload
	}
}
