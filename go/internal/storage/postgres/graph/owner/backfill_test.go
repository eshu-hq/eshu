// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ownerstore_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/graph/owner"
)

// backfillDB adds db.Beginner to fake.ExecQueryer for
// GraphNodeOwnerBackfillDB, reusing the fake's existing
// BeginReadOnlyRepeatableRead recording (BeginReadOnlyRepeatableReadCalls,
// Execs, Queries) so this test's assertions read the same fields every other
// fake.ExecQueryer-based test in this package tree uses.
type backfillDB struct {
	*fake.ExecQueryer
}

func (d *backfillDB) Begin(ctx context.Context) (db.Transaction, error) {
	return d.BeginReadOnlyRepeatableRead(ctx)
}

func TestGraphNodeOwnerBackfillStateMigrationDeclaresDurableMarker(t *testing.T) {
	t.Parallel()

	ddl := postgres.MigrationSQL("graph_node_owner_backfill_state")
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

	database := &backfillDB{ExecQueryer: &fake.ExecQueryer{}}
	store := ownerstore.NewGraphNodeOwnerBackfillStore(database)
	now := time.Date(2026, time.July, 21, 15, 0, 0, 0, time.UTC)
	entries := []ownerstore.GraphNodeOwnerEntry{{
		UID:            "uid-a",
		SourceOrderKey: "0001-01-01T00:00:00.000000000Z|fact-a",
		WinningRow:     json.RawMessage(`{"uid":"uid-a","source_fact_id":"fact-a"}`),
	}}

	if err := store.SeedExistingGraphNodeOwners(context.Background(), entries, now); err != nil {
		t.Fatalf("SeedExistingGraphNodeOwners() error = %v, want nil", err)
	}
	if got, want := database.BeginReadOnlyRepeatableReadCalls, 1; got != want {
		t.Fatalf("begin calls = %d, want %d", got, want)
	}
	if got, want := len(database.Execs), 2; got != want {
		t.Fatalf("exec calls = %d, want %d (lock + max upsert)", got, want)
	}
	if database.Execs[0].Query != ownerstore.GraphNodeOwnerAcquireLocksSQL {
		t.Fatalf("first statement did not acquire the shared per-uid locks:\n%s", database.Execs[0].Query)
	}
	if !strings.Contains(database.Execs[1].Query, ownerstore.GraphNodeOwnerUpsertSuffix) {
		t.Fatalf("seed did not use the monotonic owner max-upsert:\n%s", database.Execs[1].Query)
	}
}

func TestGraphNodeOwnerBackfillStoreStateQueriesUseStableKey(t *testing.T) {
	t.Parallel()

	database := &backfillDB{ExecQueryer: &fake.ExecQueryer{
		QueryResponses: []fake.Rows{{}},
	}}
	store := ownerstore.NewGraphNodeOwnerBackfillStore(database)
	complete, err := store.IsCloudResourceBackfillComplete(context.Background())
	if err != nil {
		t.Fatalf("IsCloudResourceBackfillComplete() error = %v", err)
	}
	if complete {
		t.Fatal("IsCloudResourceBackfillComplete() = true with no marker")
	}
	if got, want := database.Queries[0].Args[0], ownerstore.CloudResourceOwnerBackfillKey; got != want {
		t.Fatalf("completion key = %v, want %q", got, want)
	}

	now := time.Date(2026, time.July, 21, 15, 5, 0, 0, time.UTC)
	if err := store.MarkCloudResourceBackfillComplete(context.Background(), now); err != nil {
		t.Fatalf("MarkCloudResourceBackfillComplete() error = %v", err)
	}
	mark := database.Execs[len(database.Execs)-1]
	if !strings.Contains(mark.Query, "ON CONFLICT (backfill_key) DO NOTHING") {
		t.Fatalf("completion marker is not idempotent:\n%s", mark.Query)
	}
	if got, want := mark.Args[0], ownerstore.CloudResourceOwnerBackfillKey; got != want {
		t.Fatalf("mark key = %v, want %q", got, want)
	}
}
