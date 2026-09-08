// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerGraphDrainHasActiveReducerGraphWork(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{true}}}},
	}
	check := NewReducerGraphDrain(db)

	active, err := check.HasActiveReducerGraphWork(context.Background())
	if err != nil {
		t.Fatalf("HasActiveReducerGraphWork() error = %v, want nil", err)
	}
	if !active {
		t.Fatal("HasActiveReducerGraphWork() = false, want true")
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"active_fact_work_items AS (",
		"FROM fact_work_items",
		"JOIN ingestion_scopes AS scope",
		"scope.active_generation_id = active_generation.generation_id",
		"work.stage = 'reducer'",
		"work.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
		"stale_generation.ingested_at < active_generation.ingested_at",
		"stale_generation.generation_id < active_generation.generation_id",
		"FROM active_fact_work_items",
		"stage = 'reducer'",
		"status IN ('pending', 'retrying', 'claimed', 'running')",
		"domain IN (",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
}

// TestReducerGraphDrainHasUncommittedCanonicalCodeScopes pins the #6184
// cross-repository CALLS gate: a code scope whose active generation holds git
// repository facts but has not published its code_entities_uid
// canonical_nodes_committed phase must hold code-call projection back, because
// a cross-repository edge drained now MATCHes an endpoint that does not exist
// yet and is then marked completed — a silent permanent loss. Scopes with no
// repository facts (non-code scopes never publish this phase) must not block.
func TestReducerGraphDrainHasUncommittedCanonicalCodeScopes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{true}}}},
	}
	check := NewReducerGraphDrain(db)

	uncommitted, err := check.HasUncommittedCanonicalCodeScopes(context.Background())
	if err != nil {
		t.Fatalf("HasUncommittedCanonicalCodeScopes() error = %v, want nil", err)
	}
	if !uncommitted {
		t.Fatal("HasUncommittedCanonicalCodeScopes() = false, want true")
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	// Derived-from-constant guard: the asserted shape is a byte prefix of the
	// shipped query, so the test tracks production instead of a hand copy.
	if !strings.HasPrefix(query, uncommittedCanonicalCodeScopesQuery) {
		t.Fatalf("query drifted from uncommittedCanonicalCodeScopesQuery:\n%s", query)
	}
	args := db.queries[0].args
	if got, want := len(args), 2; got != want {
		t.Fatalf("query args = %d, want %d: %v", got, want, args)
	}
	if got, want := args[0], string(reducer.GraphProjectionKeyspaceCodeEntitiesUID); got != want {
		t.Fatalf("keyspace arg = %v, want %v", got, want)
	}
	if got, want := args[1], string(reducer.GraphProjectionPhaseCanonicalNodesCommitted); got != want {
		t.Fatalf("phase arg = %v, want %v", got, want)
	}
	for _, want := range []string{
		"FROM ingestion_scopes AS scope",
		"scope.active_generation_id IS NOT NULL",
		"FROM fact_records AS fact",
		"fact.fact_kind = 'repository'",
		"fact.source_system = 'git'",
		"fact.is_tombstone = FALSE",
		"FROM graph_projection_phase_state AS phase",
		"phase.keyspace = $1",
		"phase.phase = $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("query missing %q:\n%s", want, query)
		}
	}
}

func TestReducerGraphDrainHasUncommittedCanonicalCodeScopesClear(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{{false}}}},
	}
	check := NewReducerGraphDrain(db)

	uncommitted, err := check.HasUncommittedCanonicalCodeScopes(context.Background())
	if err != nil {
		t.Fatalf("HasUncommittedCanonicalCodeScopes() error = %v, want nil", err)
	}
	if uncommitted {
		t.Fatal("HasUncommittedCanonicalCodeScopes() = true, want false")
	}
}
