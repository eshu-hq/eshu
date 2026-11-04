// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql/driver"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// TestWorkloadCandidatePreconditionBindsSignalKeysAsOneArray locks the #9697
// probe to its contract without text-matching quoting: the signal keys travel
// as ONE bound text[] argument derived from
// factload.WorkloadCandidateSignalKeys, and no key text is interpolated into
// the SQL. The previous assertion (each key appears as 'key' in the query)
// passed on a statement PostgreSQL rejects as a syntax error (#9986), so this
// test asserts the arguments the store actually sends. SQL validity itself is
// proven against a real planner by TestScopeMayHoldWorkloadCandidatesLive.
func TestWorkloadCandidatePreconditionBindsSignalKeysAsOneArray(t *testing.T) {
	t.Parallel()

	store, database := probeFakeStore(true)
	if _, err := store.ScopeMayHoldWorkloadCandidates(context.Background(), "scope-1", "gen-1"); err != nil {
		t.Fatalf("ScopeMayHoldWorkloadCandidates: %v", err)
	}
	if len(database.queries) != 1 {
		t.Fatalf("queries = %d, want 1 single-statement probe", len(database.queries))
	}
	call := database.queries[0]
	if call.query != workloadCandidatePreconditionQuery() {
		t.Fatal("the store must send the shipped precondition query verbatim")
	}
	if len(call.args) != 3 {
		t.Fatalf("probe args = %v, want [scope generation keys]", call.args)
	}

	keys, ok := call.args[2].(driver.Valuer)
	if !ok {
		t.Fatalf("keys arg = %T, want a driver.Valuer text array (one bound parameter)", call.args[2])
	}
	literal, err := keys.Value()
	if err != nil {
		t.Fatalf("keys arg Value(): %v", err)
	}
	encoded, ok := literal.(string)
	if !ok {
		t.Fatalf("keys arg literal = %T, want a text array literal", literal)
	}
	want, err := array.StringArray(factload.WorkloadCandidateSignalKeys).Value()
	if err != nil {
		t.Fatalf("expected literal Value(): %v", err)
	}
	if encoded != want {
		t.Fatalf("keys arg = %q, want %q derived from factload.WorkloadCandidateSignalKeys", encoded, want)
	}
	for _, key := range factload.WorkloadCandidateSignalKeys {
		if !strings.Contains(encoded, `"`+key+`"`) {
			t.Errorf("bound array misses signal key %q", key)
		}
		if strings.Contains(call.query, key) {
			t.Errorf("query interpolates signal key %q: keys must travel only in the bound array", key)
		}
	}
}

// TestWorkloadCandidatePreconditionQueryShape pins the structural predicates
// and the parameter contract: every placeholder the SQL uses is supplied, the
// key-existence test is the bound array, and the performance shape (two
// equality partitions, key existence on the inner side, EXISTS) is kept.
func TestWorkloadCandidatePreconditionQueryShape(t *testing.T) {
	t.Parallel()

	query := workloadCandidatePreconditionQuery()
	if !strings.HasPrefix(query, "SELECT EXISTS (") {
		t.Fatalf("precondition query = %q, want the derived EXISTS prefix", query)
	}
	for _, fragment := range []string{
		"fact_kind = 'repository'",
		"fact_kind = 'file'",
		"payload->>'graph_id'",
		"payload->>'name'",
		"payload->>'repo_id'",
		"parsed_file_data'",
		"?| $3::text[]",
	} {
		if !strings.Contains(query, fragment) {
			t.Errorf("precondition query misses predicate %q", fragment)
		}
	}
	maxPlaceholder := 0
	for _, match := range regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(query, -1) {
		n, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("placeholder %q: %v", match[0], err)
		}
		if n > maxPlaceholder {
			maxPlaceholder = n
		}
	}
	if maxPlaceholder != 3 {
		t.Fatalf("highest placeholder = $%d, want $3 (scope, generation, keys)", maxPlaceholder)
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
