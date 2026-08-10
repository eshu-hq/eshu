// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func unroutableRow(intentID string) reducer.SharedProjectionUnroutableRow {
	return reducer.SharedProjectionUnroutableRow{
		IntentID:         intentID,
		ProjectionDomain: "repo_dependency",
		PartitionKey:     "pk-a",
		RepositoryID:     "repo-a",
		ScopeID:          "scope-a",
		GenerationID:     "gen-1",
		EvidenceSource:   "finalization/workloads",
		Reason:           reducer.UnroutableReasonMissingRequiredField,
		DecidedAt:        time.Date(2026, time.August, 9, 14, 0, 0, 0, time.UTC),
	}
}

// TestWriteUnroutableIntentsUpsertIsIdempotent pins the replay guard.
//
// The owning cycle re-runs whole when anything after this write fails — a
// crash between here and MarkIntentsCompleted, for instance — so the same
// intent can arrive twice. ON CONFLICT DO NOTHING keeps that converging on one
// row with the FIRST decision time, rather than duplicating or erroring.
func TestWriteUnroutableIntentsUpsertIsIdempotent(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewSharedProjectionUnroutableIntentStore(db)

	if err := store.WriteUnroutableIntents(context.Background(), []reducer.SharedProjectionUnroutableRow{
		unroutableRow("i1"),
	}); err != nil {
		t.Fatalf("WriteUnroutableIntents() error = %v", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	if !strings.Contains(query, "ON CONFLICT (intent_id) DO NOTHING") {
		t.Fatalf("insert is not replay-idempotent:\n%s", query)
	}
	if !strings.Contains(query, "INSERT INTO shared_projection_unroutable_intents") {
		t.Fatalf("insert targets the wrong table:\n%s", query)
	}
}

// TestWriteUnroutableIntentsDoesNotTouchTheFactQuarantineTable is the guard
// against the reuse that was deliberately rejected: an intent row has no fact
// identity, so recording it in reducer_input_invalid_facts would misreport what
// was quarantined and pollute a read surface the corpus gate already asserts.
func TestWriteUnroutableIntentsDoesNotTouchTheFactQuarantineTable(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewSharedProjectionUnroutableIntentStore(db)
	if err := store.WriteUnroutableIntents(context.Background(), []reducer.SharedProjectionUnroutableRow{
		unroutableRow("i1"),
	}); err != nil {
		t.Fatalf("WriteUnroutableIntents() error = %v", err)
	}
	if strings.Contains(db.execs[0].query, "reducer_input_invalid_facts") {
		t.Fatal("unroutable intents must not be written to the fact quarantine table")
	}
}

// TestWriteUnroutableIntentsBatchesLargeInputs keeps a pathological batch from
// becoming one unbounded statement.
func TestWriteUnroutableIntentsBatchesLargeInputs(t *testing.T) {
	t.Parallel()

	rows := make([]reducer.SharedProjectionUnroutableRow, 0, sharedProjectionUnroutableIntentBatchSize+10)
	for i := 0; i < cap(rows); i++ {
		rows = append(rows, unroutableRow(fmt.Sprintf("intent-%d", i)))
	}

	db := &fakeExecQueryer{}
	store := NewSharedProjectionUnroutableIntentStore(db)
	if err := store.WriteUnroutableIntents(context.Background(), rows); err != nil {
		t.Fatalf("WriteUnroutableIntents() error = %v", err)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d (batched at %d)", got, want, sharedProjectionUnroutableIntentBatchSize)
	}
}

// TestWriteUnroutableIntentsEmptyIsANoop keeps the ordinary cycle free of a
// pointless round trip.
func TestWriteUnroutableIntentsEmptyIsANoop(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewSharedProjectionUnroutableIntentStore(db)
	if err := store.WriteUnroutableIntents(context.Background(), nil); err != nil {
		t.Fatalf("WriteUnroutableIntents() error = %v", err)
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("exec count = %d, want 0", got)
	}
}

// TestSharedProjectionUnroutableIntentSchemaHasNoForeignKeys pins the
// deliberate divergence from reducer_input_invalid_facts.
//
// shared_projection_intents.scope_id is TEXT NOT NULL DEFAULT ”, so a legacy
// row can carry an empty scope. An FK to ingestion_scopes would reject the
// insert exactly when a malformed row is being recorded — turning the record of
// a loss into a second failure.
func TestSharedProjectionUnroutableIntentSchemaHasNoForeignKeys(t *testing.T) {
	t.Parallel()

	schema := SharedProjectionUnroutableIntentSchemaSQL()
	if strings.Contains(schema, "REFERENCES") {
		t.Fatalf("schema declares a foreign key; an empty scope_id would then fail the insert:\n%s", schema)
	}
	if !strings.Contains(schema, "intent_id TEXT PRIMARY KEY") {
		t.Fatalf("intent_id must be the primary key (it already encodes the natural key):\n%s", schema)
	}
}
