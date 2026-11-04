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
	if len(call.args) != 6 {
		t.Fatalf("probe args = %v, want [scope generation signal-keys array-keys presence-keys token-keys]", call.args)
	}

	for i, class := range []struct {
		name string
		keys []string
	}{
		{"signal keys", factload.WorkloadCandidateSignalKeys},
		{"array-bucket keys", factload.WorkloadCandidateArrayBucketKeys},
		{"presence keys", factload.WorkloadCandidatePresenceKeys},
		{"token keys", factload.WorkloadCandidateTokenKeys},
	} {
		arg := call.args[2+i]
		valuer, ok := arg.(driver.Valuer)
		if !ok {
			t.Fatalf("%s arg = %T, want a driver.Valuer text array (one bound parameter)", class.name, arg)
		}
		literal, err := valuer.Value()
		if err != nil {
			t.Fatalf("%s arg Value(): %v", class.name, err)
		}
		encoded, ok := literal.(string)
		if !ok {
			t.Fatalf("%s arg literal = %T, want a text array literal", class.name, literal)
		}
		want, err := array.StringArray(class.keys).Value()
		if err != nil {
			t.Fatalf("expected literal Value(): %v", err)
		}
		if encoded != want {
			t.Fatalf("%s arg = %q, want %q derived from factload", class.name, encoded, want)
		}
		for _, key := range class.keys {
			if !strings.Contains(encoded, `"`+key+`"`) {
				t.Errorf("%s array misses key %q", class.name, key)
			}
		}
	}
	for _, key := range factload.WorkloadCandidateSignalKeys {
		if strings.Contains(call.query, key) {
			t.Errorf("query interpolates signal key %q: keys must travel only in the bound arrays", key)
		}
	}
}

// TestWorkloadCandidatePreconditionQueryShape pins the structural predicates
// and the parameter contract: every placeholder the SQL uses is supplied, the
// value tests read the bound key arrays, and the performance shape (two
// equality partitions, one jsonb_to_record read on the file side, EXISTS) is kept.
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
		"jsonb_to_record(f.payload) AS x(repo_id text, relative_path text, parsed_file_data jsonb)",
		"x.repo_id = repo.payload->>'graph_id'",
		"x.relative_path ~",
		"?| $3::text[]",
		"unnest($4::text[])",
		"unnest($5::text[])",
		"unnest($6::text[])",
		"jsonb_typeof(x.parsed_file_data) = 'object'",
		"<> '[]'::jsonb",
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
	if maxPlaceholder != 6 {
		t.Fatalf("highest placeholder = $%d, want $6 (scope, generation, four key arrays)", maxPlaceholder)
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
