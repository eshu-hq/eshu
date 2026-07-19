// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

// TestDedupeReducerFactRowsByFactIDKeepsLastOccurrence proves the dedupe that
// preserves the per-row loop's last-write-wins semantics: rows sharing a fact_id
// collapse to a single row carrying the LAST occurrence's value, in order of
// last occurrence. Rows with unique fact_ids pass through unchanged.
func TestDedupeReducerFactRowsByFactIDKeepsLastOccurrence(t *testing.T) {
	t.Parallel()

	rows := []reducerFactRow{
		{FactID: "X", Payload: "v0"},
		{FactID: "Y", Payload: "y"},
		{FactID: "X", Payload: "v2"}, // later duplicate of X wins
	}
	got := dedupeReducerFactRowsByFactID(rows, func(r reducerFactRow) string { return r.FactID })
	if len(got) != 2 {
		t.Fatalf("deduped len = %d, want 2 (one row per fact_id)", len(got))
	}
	// Order is by last occurrence: Y (idx 1) then X (idx 2).
	if got[0].FactID != "Y" || got[1].FactID != "X" {
		t.Fatalf("deduped fact_ids = [%s %s], want [Y X]", got[0].FactID, got[1].FactID)
	}
	if got[1].Payload != "v2" {
		t.Fatalf("X payload = %q, want v2 (last write wins)", got[1].Payload)
	}

	// No-duplicate slice passes through unchanged (same backing array).
	unique := []reducerFactRow{{FactID: "A"}, {FactID: "B"}}
	if out := dedupeReducerFactRowsByFactID(unique, func(r reducerFactRow) string { return r.FactID }); len(out) != 2 {
		t.Fatalf("unique passthrough len = %d, want 2", len(out))
	}
}

// TestReducerBatchInsertFactsDedupesDuplicateFactID proves the shared batch
// insert (used by all migrated writers) never emits the same fact_id twice in
// one chunk — which would trip Postgres' "ON CONFLICT DO UPDATE command cannot
// affect row a second time" and wedge the leased projection intent — and keeps
// the last write's value, matching the per-row loop it replaced (#2809/#2855).
func TestReducerBatchInsertFactsDedupesDuplicateFactID(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()
	rows := []reducerFactRow{
		{FactID: "dup", FactKind: "k", StableFactKey: "sk1", Payload: `{"v":0}`, ObservedAt: now, IngestedAt: now},
		{FactID: "other", FactKind: "k", StableFactKey: "sk2", Payload: `{"v":9}`, ObservedAt: now, IngestedAt: now},
		{FactID: "dup", FactKind: "k", StableFactKey: "sk3", Payload: `{"v":2}`, ObservedAt: now, IngestedAt: now},
	}
	exec := &fakeWorkloadIdentityExecer{}
	if err := reducerBatchInsertFacts(context.Background(), exec, rows); err != nil {
		t.Fatalf("reducerBatchInsertFacts error = %v", err)
	}
	decoded := decodeBatchedFactCalls(t, exec.execs)
	if len(decoded) != 2 {
		t.Fatalf("emitted rows = %d, want 2 (deduped)", len(decoded))
	}
	seen := map[string]string{}
	for _, r := range decoded {
		if prev, dup := seen[r.FactID]; dup {
			t.Fatalf("fact_id %q emitted twice in one batch (was %q) — ON CONFLICT would reject", r.FactID, prev)
		}
		seen[r.FactID] = string(r.Payload)
		// F2: source_uri/source_record_id are nil (unset) and round-trip as nil.
		if r.SourceURI != nil {
			t.Fatalf("fact_id %q source_uri = %v, want nil", r.FactID, *r.SourceURI)
		}
		if r.SourceRecordID != nil {
			t.Fatalf("fact_id %q source_record_id = %v, want nil", r.FactID, *r.SourceRecordID)
		}
	}
	if seen["dup"] != `{"v":2}` {
		t.Fatalf("dup payload = %q, want the last write {\"v\":2}", seen["dup"])
	}
}

// TestReducerBatchInsertVersionedFactsDedupesDuplicateFactID is the versioned
// (governed-fact) counterpart of the dedupe guard.
func TestReducerBatchInsertVersionedFactsDedupesDuplicateFactID(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0).UTC()
	rows := []reducerFactVersionedRow{
		{FactID: "dup", FactKind: "k", StableFactKey: "sk1", SchemaVersion: "1.0.0", Payload: `{"v":0}`, ObservedAt: now, IngestedAt: now},
		{FactID: "dup", FactKind: "k", StableFactKey: "sk2", SchemaVersion: "1.0.0", Payload: `{"v":1}`, ObservedAt: now, IngestedAt: now},
	}
	exec := &fakeWorkloadIdentityExecer{}
	if err := reducerBatchInsertVersionedFacts(context.Background(), exec, rows); err != nil {
		t.Fatalf("reducerBatchInsertVersionedFacts error = %v", err)
	}
	decoded := decodeBatchedVersionedFactCalls(t, exec.execs)
	if len(decoded) != 1 {
		t.Fatalf("emitted versioned rows = %d, want 1 (deduped)", len(decoded))
	}
	if string(decoded[0].Payload) != `{"v":1}` {
		t.Fatalf("versioned dup payload = %q, want last write {\"v\":1}", decoded[0].Payload)
	}
	if decoded[0].SchemaVersion != "1.0.0" {
		t.Fatalf("versioned schema_version = %q, want 1.0.0", decoded[0].SchemaVersion)
	}
}
