// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"reflect"
	"testing"
)

func TestOwnerEntriesFromRowsCarriesUIDOrderKeyAndJSON(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"uid": "a", "source_order_key": "2026-01-01|f1", "name": "main"},
		{"uid": "b", "source_order_key": "2026-01-02|f2", "state": "available"},
	}
	entries, err := ownerEntriesFromRows(rows)
	if err != nil {
		t.Fatalf("ownerEntriesFromRows error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].UID != "a" || entries[0].SourceOrderKey != "2026-01-01|f1" {
		t.Fatalf("entry 0 = %+v, want uid=a key=2026-01-01|f1", entries[0])
	}
	if len(entries[0].WinningRow) == 0 {
		t.Fatal("entry 0 WinningRow (JSONB) must be populated for the Stage 2 foundation")
	}
}

func TestFilterOwnedRowsReturnsSameSliceWhenAllOwned(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"uid": "a"}, {"uid": "b"}, {"uid": "c"}}
	owned := map[string]struct{}{"a": {}, "b": {}, "c": {}}
	got := filterOwnedRows(rows, owned)
	// Non-contended common case: identical slice, so the graph write is
	// byte-identical to the un-gated write.
	if !reflect.DeepEqual(got, rows) {
		t.Fatalf("all-owned filter changed the rows: got %v", got)
	}
}

func TestFilterOwnedRowsDropsLostUIDs(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"uid": "a"}, {"uid": "b"}, {"uid": "c"}}
	owned := map[string]struct{}{"a": {}, "c": {}}
	got := filterOwnedRows(rows, owned)
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (a, c)", len(got))
	}
	if got[0]["uid"] != "a" || got[1]["uid"] != "c" {
		t.Fatalf("filtered rows = %v, want [a c] in order", got)
	}
}

func TestFilterOwnedRowsEmptyOwnedDropsAll(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"uid": "a"}, {"uid": "b"}}
	got := filterOwnedRows(rows, map[string]struct{}{})
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0 when nothing owned", len(got))
	}
}

// TestNilGateWritesThrough proves a nil/ledger-less gate preserves prior
// behavior by writing every row through unchanged (the pass-through path).
func TestNilGateWritesThrough(t *testing.T) {
	t.Parallel()

	var got []map[string]any
	underlying := func(_ context.Context, rows []map[string]any, _ string) error {
		got = rows
		return nil
	}
	rows := []map[string]any{{"uid": "a"}, {"uid": "b"}}

	// Nil db => pass-through.
	gate := &Gate{}
	w := NewCloudResourceGatedWriter(gate, underlying)
	if err := w.WriteCloudResourceNodes(context.Background(), rows, "reducer/aws-resources"); err != nil {
		t.Fatalf("WriteCloudResourceNodes error = %v", err)
	}
	if !reflect.DeepEqual(got, rows) {
		t.Fatalf("pass-through gate altered rows: got %v", got)
	}
}

// TestEmptyRowsWritesThrough proves an empty batch is a pass-through no-op that
// never opens a transaction.
func TestEmptyRowsWritesThrough(t *testing.T) {
	t.Parallel()

	called := false
	underlying := func(_ context.Context, rows []map[string]any, _ string) error {
		called = true
		if len(rows) != 0 {
			t.Fatalf("underlying called with %d rows, want 0", len(rows))
		}
		return nil
	}
	gate := NewGate(nil)
	w := NewEC2InstanceGatedWriter(gate, underlying)
	if err := w.WriteEC2InstanceNodes(context.Background(), nil, "reducer/ec2-instances"); err != nil {
		t.Fatalf("WriteEC2InstanceNodes error = %v", err)
	}
	if !called {
		t.Fatal("underlying was not called for the empty-batch no-op")
	}
}
