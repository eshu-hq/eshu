// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGraphNodeOwnerBackfillStateMigrationDeclaresDurableMarker(t *testing.T) {
	t.Parallel()

	ddl := MigrationSQL("graph_node_owner_backfill_state")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS graph_node_owner_backfill_state",
		"backfill_key TEXT PRIMARY KEY",
		"completed_at TIMESTAMPTZ NOT NULL",
	} {
		if !strings.Contains(ddl, want) {
			t.Fatalf("backfill state migration missing %q:\n%s", want, ddl)
		}
	}
}

func TestGraphNodeOwnerBackfillStoreSeedUsesLockedMaxUpsert(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewGraphNodeOwnerBackfillStore(db)
	now := time.Date(2026, time.July, 21, 15, 0, 0, 0, time.UTC)
	entries := []GraphNodeOwnerEntry{{
		UID:            "uid-a",
		SourceOrderKey: "0001-01-01T00:00:00.000000000Z|fact-a",
		WinningRow:     json.RawMessage(`{"uid":"uid-a","source_fact_id":"fact-a"}`),
	}}

	if err := store.SeedExistingGraphNodeOwners(context.Background(), entries, now); err != nil {
		t.Fatalf("SeedExistingGraphNodeOwners() error = %v, want nil", err)
	}
	if got, want := db.beginCalls, 1; got != want {
		t.Fatalf("begin calls = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec calls = %d, want %d (lock + max upsert)", got, want)
	}
	if db.execs[0].query != graphNodeOwnerAcquireLocksSQL {
		t.Fatalf("first statement did not acquire the shared per-uid locks:\n%s", db.execs[0].query)
	}
	if !strings.Contains(db.execs[1].query, graphNodeOwnerUpsertSuffix) {
		t.Fatalf("seed did not use the monotonic owner max-upsert:\n%s", db.execs[1].query)
	}
}

func TestGraphNodeOwnerBackfillStoreStateQueriesUseStableKey(t *testing.T) {
	t.Parallel()

	db := &recordingExecQueryer{}
	store := NewGraphNodeOwnerBackfillStore(db)
	complete, err := store.IsCloudResourceBackfillComplete(context.Background())
	if err != nil {
		t.Fatalf("IsCloudResourceBackfillComplete() error = %v", err)
	}
	if complete {
		t.Fatal("IsCloudResourceBackfillComplete() = true with no marker")
	}
	if got, want := db.queries[0].args[0], cloudResourceOwnerBackfillKey; got != want {
		t.Fatalf("completion key = %v, want %q", got, want)
	}

	now := time.Date(2026, time.July, 21, 15, 5, 0, 0, time.UTC)
	if err := store.MarkCloudResourceBackfillComplete(context.Background(), now); err != nil {
		t.Fatalf("MarkCloudResourceBackfillComplete() error = %v", err)
	}
	mark := db.execs[len(db.execs)-1]
	if !strings.Contains(mark.query, "ON CONFLICT (backfill_key) DO NOTHING") {
		t.Fatalf("completion marker is not idempotent:\n%s", mark.query)
	}
	if got, want := mark.args[0], cloudResourceOwnerBackfillKey; got != want {
		t.Fatalf("mark key = %v, want %q", got, want)
	}
}
