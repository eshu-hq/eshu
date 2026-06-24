// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestUpsertScopeGenerationQueryPersistsIsDelta(t *testing.T) {
	t.Parallel()

	if !strings.Contains(upsertScopeGenerationQuery, "is_delta") {
		t.Fatalf("upsertScopeGenerationQuery must persist is_delta:\n%s", upsertScopeGenerationQuery)
	}
	if !strings.Contains(upsertScopeGenerationQuery, "is_delta = EXCLUDED.is_delta") {
		t.Fatalf("upsertScopeGenerationQuery must update is_delta on conflict:\n%s", upsertScopeGenerationQuery)
	}
}

func TestLastFullProjectionAtReturnsTimestamp(t *testing.T) {
	t.Parallel()

	want := time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{{want}}}}}
	store := IngestionStore{db: &projectedCommitTestDB{queryer: queryer}}

	got, ok, err := store.LastFullProjectionAt(context.Background(), "git-repository-scope:acme/app")
	if err != nil {
		t.Fatalf("LastFullProjectionAt() error = %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if !got.Equal(want) {
		t.Fatalf("got = %v, want %v", got, want)
	}

	q := queryer.queries[0]
	for _, fragment := range []string{
		"ingested_at",
		"scope_id = $1",
		"'active', 'completed', 'superseded'",
		"is_delta = false",
		"LIMIT 1",
	} {
		if !strings.Contains(q, fragment) {
			t.Fatalf("lastFullProjectionAtQuery missing %q:\n%s", fragment, q)
		}
	}
}

func TestLastFullProjectionAtAbsentWhenNoFullGeneration(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: [][]any{}}}}
	store := IngestionStore{db: &projectedCommitTestDB{queryer: queryer}}

	_, ok, err := store.LastFullProjectionAt(context.Background(), "git-repository-scope:acme/app")
	if err != nil {
		t.Fatalf("LastFullProjectionAt() error = %v", err)
	}
	if ok {
		t.Fatal("ok = true, want false (no full projection yet)")
	}
}

func TestLastFullProjectionAtBlankScopeAbsent(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{}
	store := IngestionStore{db: &projectedCommitTestDB{queryer: queryer}}

	_, ok, err := store.LastFullProjectionAt(context.Background(), "  ")
	if err != nil {
		t.Fatalf("LastFullProjectionAt() error = %v", err)
	}
	if ok {
		t.Fatal("ok = true, want false for blank scope")
	}
	if len(queryer.queries) != 0 {
		t.Fatalf("blank scope must not query, got %d", len(queryer.queries))
	}
}
