// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestGraphNodeOwnerSchemaSQLMatchesMigration proves the store resolves its DDL
// from the embedded migration (single source of truth) and that the migration
// declares the expected table and primary key.
func TestGraphNodeOwnerSchemaSQLMatchesMigration(t *testing.T) {
	t.Parallel()

	ddl, err := graphNodeOwnerSchemaSQL()
	if err != nil {
		t.Fatalf("graphNodeOwnerSchemaSQL() error = %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS graph_node_owner",
		"uid TEXT PRIMARY KEY",
		"source_order_key TEXT NOT NULL",
		"winning_row JSONB NOT NULL",
		"updated_at TIMESTAMPTZ NOT NULL",
	} {
		if !strings.Contains(ddl, want) {
			t.Fatalf("graph_node_owner DDL missing %q:\n%s", want, ddl)
		}
	}
}

// TestGraphNodeOwnerUpsertKeepsMaxOrderKey proves the upsert SQL only overwrites
// when the incoming order key is strictly greater — the atomic max resolution.
func TestGraphNodeOwnerUpsertKeepsMaxOrderKey(t *testing.T) {
	t.Parallel()

	if !strings.Contains(graphNodeOwnerUpsertSuffix, "ON CONFLICT (uid) DO UPDATE") {
		t.Fatalf("upsert must be keyed on uid:\n%s", graphNodeOwnerUpsertSuffix)
	}
	if !strings.Contains(graphNodeOwnerUpsertSuffix, "WHERE EXCLUDED.source_order_key > graph_node_owner.source_order_key") {
		t.Fatalf("upsert must keep the strictly-greater order key:\n%s", graphNodeOwnerUpsertSuffix)
	}
}

// TestGraphNodeOwnerAcquireLocksSortsBeforeLocking proves the lock statement
// materializes the sorted DISTINCT key set before applying the lock function, so
// concurrent overlapping batches acquire shared locks in the same order and
// cannot deadlock (concurrency-deadlock-rigor: consistent lock ordering).
func TestGraphNodeOwnerAcquireLocksSortsBeforeLocking(t *testing.T) {
	t.Parallel()

	sql := graphNodeOwnerAcquireLocksSQL
	if !strings.Contains(sql, "pg_advisory_xact_lock(k)") {
		t.Fatalf("must use transaction-scoped advisory locks:\n%s", sql)
	}
	// The ORDER BY must sit inside the subquery feeding the lock function, not
	// after it, so the lock function is applied in sorted order.
	inner := sql[strings.Index(sql, "FROM (")+len("FROM ("):]
	if !strings.Contains(inner, "ORDER BY k") {
		t.Fatalf("ORDER BY k must be inside the subquery so locks acquire in sorted order:\n%s", sql)
	}
	if !strings.Contains(inner, "DISTINCT k") {
		t.Fatalf("keys must be de-duplicated before locking:\n%s", sql)
	}
}

func TestGraphNodeOwnerAdvisoryKeyIsDeterministicAndNamespaced(t *testing.T) {
	t.Parallel()

	a := graphNodeOwnerAdvisoryKey("cloud:aws:vpc-1")
	b := graphNodeOwnerAdvisoryKey("cloud:aws:vpc-1")
	c := graphNodeOwnerAdvisoryKey("cloud:aws:vpc-2")
	if a != b {
		t.Fatalf("advisory key not deterministic: %d != %d", a, b)
	}
	if a == c {
		t.Fatalf("distinct uids collided on advisory key %d", a)
	}
	if a < 0 {
		t.Fatalf("advisory key must be non-negative (63-bit), got %d", a)
	}
	// A different subsystem prefix must produce a different key for the same id.
	if graphNodeOwnerAdvisoryKey("x") == packageRegistryIdentityAdvisoryLockKey("x") {
		t.Fatal("graph node owner advisory key collides with package registry identity namespace")
	}
}

// TestDedupeOwnerEntriesCollapsesToMaxOrderKeyAndSorts proves the within-batch
// dedup keeps the max order key per uid, drops blanks, and returns entries
// sorted by uid (so lock/upsert order is deterministic).
func TestDedupeOwnerEntriesCollapsesToMaxOrderKeyAndSorts(t *testing.T) {
	t.Parallel()

	entries := []GraphNodeOwnerEntry{
		{UID: "b", SourceOrderKey: "1000"},
		{UID: "a", SourceOrderKey: "1000"},
		{UID: "b", SourceOrderKey: "3000"}, // higher — must win for b
		{UID: "b", SourceOrderKey: "2000"},
		{UID: "  ", SourceOrderKey: "9999"}, // blank — dropped
		{UID: "a", SourceOrderKey: "0500"},
	}
	got := dedupeOwnerEntries(entries)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (a, b)", len(got))
	}
	if got[0].UID != "a" || got[1].UID != "b" {
		t.Fatalf("entries not sorted by uid: %v", got)
	}
	if got[1].SourceOrderKey != "3000" {
		t.Fatalf("b did not collapse to max order key: got %q, want 3000", got[1].SourceOrderKey)
	}
	if got[0].SourceOrderKey != "1000" {
		t.Fatalf("a did not collapse to max order key: got %q, want 1000", got[0].SourceOrderKey)
	}
}

func TestDedupeOwnerEntriesEmptyReturnsNil(t *testing.T) {
	t.Parallel()

	if got := dedupeOwnerEntries(nil); got != nil {
		t.Fatalf("dedupeOwnerEntries(nil) = %v, want nil", got)
	}
	if got := dedupeOwnerEntries([]GraphNodeOwnerEntry{{UID: "  "}}); got != nil {
		t.Fatalf("dedupeOwnerEntries(blank) = %v, want nil", got)
	}
}

// TestResolveOwnedUIDsRejectsNilTxAndZeroTime proves the fail-closed guards.
func TestResolveOwnedUIDsRejectsNilTxAndZeroTime(t *testing.T) {
	t.Parallel()

	store := NewGraphNodeOwnerStore()
	if _, _, err := store.ResolveOwnedUIDs(t.Context(), nil, []GraphNodeOwnerEntry{{UID: "a", SourceOrderKey: "1"}}, time.Now().UTC()); err == nil {
		t.Fatal("ResolveOwnedUIDs(nil tx) = nil error, want error")
	}
	rec := &recordingExecQueryer{}
	if _, _, err := store.ResolveOwnedUIDs(t.Context(), rec, []GraphNodeOwnerEntry{{UID: "a", SourceOrderKey: "1"}}, time.Time{}); err == nil {
		t.Fatal("ResolveOwnedUIDs(zero time) = nil error, want error")
	}
}

// TestResolveOwnedUIDsEmptyEntriesIsNoOp proves an empty/blank batch touches the
// database not at all and owns nothing.
func TestResolveOwnedUIDsEmptyEntriesIsNoOp(t *testing.T) {
	t.Parallel()

	store := NewGraphNodeOwnerStore()
	rec := &recordingExecQueryer{}
	owned, lost, err := store.ResolveOwnedUIDs(t.Context(), rec, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("ResolveOwnedUIDs(empty) error = %v", err)
	}
	if len(owned) != 0 || lost != 0 {
		t.Fatalf("empty batch owned=%v lost=%d, want none", owned, lost)
	}
	if len(rec.execs) != 0 || len(rec.queries) != 0 {
		t.Fatalf("empty batch touched the database: execs=%d queries=%d", len(rec.execs), len(rec.queries))
	}
}

// TestGraphNodeOwnerEntryWinningRowIsJSON proves the entry carries JSON-encoded
// rows (the Stage 2 foundation) without the store needing to interpret them.
func TestGraphNodeOwnerEntryWinningRowIsJSON(t *testing.T) {
	t.Parallel()

	row := map[string]any{"uid": "a", "name": "main", "state": "available"}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	entry := GraphNodeOwnerEntry{UID: "a", SourceOrderKey: "1", WinningRow: raw}
	if !json.Valid(entry.WinningRow) {
		t.Fatal("WinningRow is not valid JSON")
	}
}
