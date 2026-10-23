// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func deltaUnionLoadWork(generationID string) projector.ScopeGenerationWork {
	return projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-1"},
		Generation: scope.ScopeGeneration{ScopeID: "scope-1", GenerationID: generationID},
	}
}

// #8779: LoadProjectionFacts names the base the overlay was built from, in
// the same read, with four queries and no second resolve.
func TestFactStoreLoadProjectionFactsNamesUnionBase(t *testing.T) {
	t.Parallel()
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-base", ""}}},
			{rows: [][]any{{"gen-base-1", "active"}}},
			{rows: deltaUnionBaseRows()},
		},
	}
	loaded, err := NewFactStore(db).LoadProjectionFacts(context.Background(), deltaUnionLoadWork("gen-delta-1"))
	if err != nil {
		t.Fatalf("LoadProjectionFacts() error = %v, want nil", err)
	}
	if loaded.UnionBaseGenerationID != "gen-base-1" || loaded.UnionOutcome != projector.DeltaUnionBaseFound {
		t.Fatalf("union base = %q, outcome = %q; want gen-base-1 and %q",
			loaded.UnionBaseGenerationID, loaded.UnionOutcome, projector.DeltaUnionBaseFound)
	}
	generations := map[string]bool{}
	for _, env := range loaded.Facts {
		generations[env.GenerationID] = true
	}
	if !generations["gen-base-1"] || !generations["gen-delta-1"] || len(generations) != 2 {
		t.Fatalf("loaded generations = %v, want exactly the delta and its base", generations)
	}
	if got, want := len(db.queries), 4; got != want {
		t.Fatalf("query count = %d, want %d (no second resolve)", got, want)
	}
	if loaded.UnionChained {
		t.Fatal("a delta whose union base is its baseline loaded as chained")
	}
}

// #8780: a delta diffed over an active delta loads as chained, the mark the
// build_projection stage log reports.
func TestFactStoreLoadProjectionFactsMarksChainedDelta(t *testing.T) {
	t.Parallel()
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-active-delta", "commit-base"}}},
			{rows: [][]any{{"gen-base-1", "active"}}},
			{rows: deltaUnionBaseRows()},
		},
	}
	loaded, err := NewFactStore(db).LoadProjectionFacts(context.Background(), deltaUnionLoadWork("gen-delta-1"))
	if err != nil || !loaded.UnionChained || loaded.UnionBaseGenerationID != "gen-base-1" {
		t.Fatalf("LoadProjectionFacts() = chained %v base %q, %v; want a chained union over gen-base-1",
			loaded.UnionChained, loaded.UnionBaseGenerationID, err)
	}
}

func TestFactStoreLoadProjectionFactsFullGenerationNamesNoBase(t *testing.T) {
	t.Parallel()
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{false, "", ""}}},
		},
	}
	loaded, err := NewFactStore(db).LoadProjectionFacts(context.Background(), deltaUnionLoadWork("gen-full-1"))
	if err != nil {
		t.Fatalf("LoadProjectionFacts() error = %v, want nil", err)
	}
	if loaded.UnionBaseGenerationID != "" || loaded.UnionOutcome != "" || len(loaded.Facts) != 2 {
		t.Fatalf("full load = base %q, outcome %q, %d facts; want no union and 2 facts",
			loaded.UnionBaseGenerationID, loaded.UnionOutcome, len(loaded.Facts))
	}
}

// The additive scope-mismatch overlay still loads for gate and replay readers,
// but the result carries the outcome so the projector refuses it.
func TestFactStoreLoadProjectionFactsReportsScopeMismatch(t *testing.T) {
	t.Parallel()
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionNonDeltaScopeRows()},
			{rows: [][]any{{true, "commit-base", ""}}},
			{rows: [][]any{{"gen-base-1", "active"}}},
			{rows: deltaUnionBaseRows()},
		},
	}
	loaded, err := NewFactStore(db).LoadProjectionFacts(context.Background(), deltaUnionLoadWork("gen-delta-1"))
	if err != nil {
		t.Fatalf("LoadProjectionFacts() error = %v, want nil", err)
	}
	if loaded.UnionOutcome != projector.DeltaUnionScopeMismatch || loaded.UnionBaseGenerationID != "gen-base-1" {
		t.Fatalf("outcome = %q, base = %q; want %q over gen-base-1",
			loaded.UnionOutcome, loaded.UnionBaseGenerationID, projector.DeltaUnionScopeMismatch)
	}
	if _, err := loaded.ProjectionGeneration(deltaUnionLoadWork("gen-delta-1").Generation); err == nil {
		t.Fatal("ProjectionGeneration(scope-mismatch load) error = nil, want a refusal before any write")
	}
}
