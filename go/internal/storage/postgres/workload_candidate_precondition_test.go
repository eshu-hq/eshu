// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// TestWorkloadCandidatePreconditionQueryDerivesFromSignalKeys locks the #9697
// probe to its contract: the shipped query must carry every signal key the
// probe promises, derived from factload.WorkloadCandidateSignalKeys rather
// than hand-copied, so a key added to the list cannot silently miss the SQL.
func TestWorkloadCandidatePreconditionQueryDerivesFromSignalKeys(t *testing.T) {
	t.Parallel()

	query := workloadCandidatePreconditionQuery()
	if !strings.HasPrefix(query, "SELECT EXISTS (") {
		t.Fatalf("precondition query = %q, want the derived EXISTS prefix", query)
	}
	for _, key := range factload.WorkloadCandidateSignalKeys {
		if !strings.Contains(query, "'"+key+"'") {
			t.Errorf("precondition query misses signal key %q: the SQL must derive from WorkloadCandidateSignalKeys", key)
		}
	}
	for _, fragment := range []string{
		"fact_kind = 'repository'",
		"fact_kind = 'file'",
		"payload->>'graph_id'",
		"payload->>'name'",
		"payload->>'repo_id'",
		"parsed_file_data",
	} {
		if !strings.Contains(query, fragment) {
			t.Errorf("precondition query misses structural predicate %q", fragment)
		}
	}
}

func probeFakeStore(mayHold bool) (*FactStore, *fakeExecQueryer) {
	database := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{mayHold}}},
		},
	}
	return &FactStore{database: database}, database
}

// TestScopeMayHoldWorkloadCandidatesTrue serves a true probe row and expects
// true with the scope/generation arguments passed through.
func TestScopeMayHoldWorkloadCandidatesTrue(t *testing.T) {
	t.Parallel()

	store, database := probeFakeStore(true)
	mayHold, err := store.ScopeMayHoldWorkloadCandidates(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("ScopeMayHoldWorkloadCandidates: %v", err)
	}
	if !mayHold {
		t.Fatal("mayHold = false, want true for a true probe row")
	}
	if len(database.queries) != 1 {
		t.Fatalf("queries = %d, want 1 single-statement probe", len(database.queries))
	}
	got := database.queries[0]
	if len(got.args) < 2 || got.args[0] != "scope-1" || got.args[1] != "gen-1" {
		t.Fatalf("probe args = %v, want [scope-1 gen-1]", got.args)
	}
}

// TestScopeMayHoldWorkloadCandidatesFalse serves a false probe row and
// expects false: the caller takes the vacuous success path.
func TestScopeMayHoldWorkloadCandidatesFalse(t *testing.T) {
	t.Parallel()

	store, _ := probeFakeStore(false)
	mayHold, err := store.ScopeMayHoldWorkloadCandidates(context.Background(), "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("ScopeMayHoldWorkloadCandidates: %v", err)
	}
	if mayHold {
		t.Fatal("mayHold = true, want false for a false probe row")
	}
}
