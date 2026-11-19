// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Unchanged-fact skip proof for issue #10873 (parent epic #10860).
//
// Saving a fact that is already stored rewrites its row even when nothing in
// it changed: the save stamps both timestamps from the incoming copy and both
// are indexed. On QA the fact table took 88.3M updates with ~0 done in place.
// The guard under test (upsertFactBatchSuffix) must skip a same-token replay
// whose every compared column already matches, keep the old timestamps, and
// still report the fact_id as accepted so derived data keeps flowing.
//
// These tests drive the real production statements against a live Postgres
// instance and are skipped when no DSN is configured so the hermetic unit
// suite stays green without Postgres.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func unchangedSkipProofDSN() string {
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
}

// openUnchangedSkipSchema creates an isolated throwaway schema with the
// ingestion_scopes/scope_generations/fact_records DDL. Mirrors
// openFactCrossBatchFencingSchema so this proof follows the package's
// established live-Postgres pattern.
func openUnchangedSkipSchema(t testing.TB, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	schemaName := fmt.Sprintf("fact_unchanged_%d", time.Now().UnixNano())

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create unchanged schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	for _, stmt := range []string{
		MigrationSQL("ingestion_scopes"),
		MigrationSQL("scope_generations"),
		MigrationSQL("fact_work_items"),
		factRecordSchemaSQL,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("apply unchanged schema: %v", err)
		}
	}
	return db
}

func seedUnchangedSkipScope(t testing.TB, ctx context.Context, db *sql.DB, scopeID, generationID string, now time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, 'repo', 'git', $1, NULL, 'git', $1, $2, $2, 'active', $3, '{}'::jsonb)`,
		scopeID, now, generationID); err != nil {
		t.Fatalf("insert unchanged scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ($1, $2, 'snapshot', 'unchanged-proof', $3, $3, 'active', $3, NULL, '{}'::jsonb)`,
		generationID, scopeID, now); err != nil {
		t.Fatalf("insert unchanged generation: %v", err)
	}
}

// unchangedSkipEnvelope builds a fully populated envelope; every guarded
// column is set so the change matrix can flip exactly one at a time.
func unchangedSkipEnvelope(factID, scopeID, generationID string, token int64, observedAt time.Time) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         "repository",
		StableFactKey:    "repository:" + scopeID,
		SchemaVersion:    "1.2.3",
		CollectorKind:    "git",
		FencingToken:     token,
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       observedAt,
		Payload:          map[string]any{"version": "v1", "nested": map[string]any{"n": float64(1)}},
		SourceRef: facts.Ref{
			SourceSystem:   "git",
			FactKey:        "key-1",
			SourceURI:      "https://example.invalid/repo",
			SourceRecordID: "rec-1",
		},
	}
}

type unchangedSkipRow struct {
	xmin       string
	observedAt time.Time
	ingestedAt time.Time
	token      int64
	tombstone  bool
	payload    string
}

func readUnchangedSkipRow(t *testing.T, ctx context.Context, db *sql.DB, factID string) unchangedSkipRow {
	t.Helper()
	var row unchangedSkipRow
	if err := db.QueryRowContext(
		ctx,
		`SELECT xmin::text, observed_at, ingested_at, fencing_token, is_tombstone, payload::text
		 FROM fact_records WHERE fact_id = $1`, factID,
	).Scan(&row.xmin, &row.observedAt, &row.ingestedAt, &row.token, &row.tombstone, &row.payload); err != nil {
		t.Fatalf("read unchanged row %q: %v", factID, err)
	}
	return row
}

// TestUpsertFactBatchSkipsUnchangedRow is the issue #10873 gate: replaying an
// identical fact with a newer observation time must write nothing — the tuple
// version (xmin), both timestamps, and the payload stay exactly as stored —
// while the RETURNING discriminator still reports the fact_id as accepted so
// afterBatch derivation is not starved.
func TestUpsertFactBatchSkipsUnchangedRow(t *testing.T) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the unchanged-skip proof")
	}

	ctx := context.Background()
	db := openUnchangedSkipSchema(t, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(t, ctx, db, "scope-unchanged", "gen-unchanged", now)

	const factID = "fact-unchanged-1"
	first := unchangedSkipEnvelope(factID, "scope-unchanged", "gen-unchanged", 10, now)
	if err := upsertFactBatch(ctx, store, []facts.Envelope{first}); err != nil {
		t.Fatalf("upsertFactBatch(first) error = %v, want nil", err)
	}
	stored := readUnchangedSkipRow(t, ctx, db, factID)

	replay := unchangedSkipEnvelope(factID, "scope-unchanged", "gen-unchanged", 10, now.Add(2*time.Hour))
	accepted, err := upsertFactBatchReturningAccepted(ctx, store, []facts.Envelope{replay})
	if err != nil {
		t.Fatalf("upsertFactBatchReturningAccepted(replay) error = %v, want nil", err)
	}
	if _, ok := accepted[factID]; !ok {
		t.Fatalf("replayed unchanged fact %q missing from accepted set: derived data would starve", factID)
	}

	after := readUnchangedSkipRow(t, ctx, db, factID)
	if after.xmin != stored.xmin {
		t.Fatalf("xmin moved on unchanged replay (%s -> %s): the row was rewritten", stored.xmin, after.xmin)
	}
	if !after.observedAt.Equal(stored.observedAt) {
		t.Fatalf("observed_at moved on unchanged replay (%v -> %v): want last-changed value kept", stored.observedAt, after.observedAt)
	}
	if !after.ingestedAt.Equal(stored.ingestedAt) {
		t.Fatalf("ingested_at moved on unchanged replay (%v -> %v): want last-changed value kept", stored.ingestedAt, after.ingestedAt)
	}
}

// TestUpsertFactBatchUnchangedGuardChangeMatrix proves the guard compares
// every guarded column: flipping exactly one writes, replaying exactly equal
// skips. NULL transitions on the two nullable columns are covered in both
// directions.
func TestUpsertFactBatchUnchangedGuardChangeMatrix(t *testing.T) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the unchanged-skip proof")
	}

	ctx := context.Background()
	db := openUnchangedSkipSchema(t, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(t, ctx, db, "scope-matrix", "gen-matrix", now)

	mutate := map[string]func(*facts.Envelope){
		"fact_kind":          func(e *facts.Envelope) { e.FactKind = "repository_mirror" },
		"stable_fact_key":    func(e *facts.Envelope) { e.StableFactKey = "repository:other" },
		"schema_version":     func(e *facts.Envelope) { e.SchemaVersion = "2.0.0" },
		"collector_kind":     func(e *facts.Envelope) { e.CollectorKind = "git-mirror" },
		"source_confidence":  func(e *facts.Envelope) { e.SourceConfidence = facts.SourceConfidenceReported },
		"source_system":      func(e *facts.Envelope) { e.SourceRef.SourceSystem = "git-fork" },
		"source_fact_key":    func(e *facts.Envelope) { e.SourceRef.FactKey = "key-2" },
		"source_uri_change":  func(e *facts.Envelope) { e.SourceRef.SourceURI = "https://example.invalid/other" },
		"source_uri_to_null": func(e *facts.Envelope) { e.SourceRef.SourceURI = "" },
		"source_uri_from_null": func(e *facts.Envelope) {
			e.SourceRef.SourceURI = "https://example.invalid/other"
		},
		"source_record_change": func(e *facts.Envelope) { e.SourceRef.SourceRecordID = "rec-2" },
		"source_record_to_null": func(e *facts.Envelope) {
			e.SourceRef.SourceRecordID = ""
		},
		"source_record_from_null": func(e *facts.Envelope) {
			e.SourceRef.SourceRecordID = "rec-2"
		},
		"is_tombstone": func(e *facts.Envelope) { e.IsTombstone = true },
		"payload":      func(e *facts.Envelope) { e.Payload = map[string]any{"version": "v2"} },
	}

	for name, flip := range mutate {
		t.Run(name, func(t *testing.T) {
			factID := "fact-matrix-" + strings.ReplaceAll(name, "_", "-")
			first := unchangedSkipEnvelope(factID, "scope-matrix", "gen-matrix", 10, now)
			if name == "source_uri_from_null" {
				first.SourceRef.SourceURI = ""
			}
			if name == "source_record_from_null" {
				first.SourceRef.SourceRecordID = ""
			}
			if err := upsertFactBatch(ctx, store, []facts.Envelope{first}); err != nil {
				t.Fatalf("upsertFactBatch(first) error = %v, want nil", err)
			}
			stored := readUnchangedSkipRow(t, ctx, db, factID)

			changed := first.Clone()
			changed.ObservedAt = now.Add(time.Hour)
			flip(&changed)
			if err := upsertFactBatch(ctx, store, []facts.Envelope{changed}); err != nil {
				t.Fatalf("upsertFactBatch(changed) error = %v, want nil", err)
			}
			if got := readUnchangedSkipRow(t, ctx, db, factID); got.xmin == stored.xmin {
				t.Fatalf("single-column change %q did not write: xmin stayed %s", name, stored.xmin)
			}

			// Only timestamps move on the equal replay: the guard must skip.
			before := readUnchangedSkipRow(t, ctx, db, factID)
			replay := changed.Clone()
			replay.ObservedAt = now.Add(2 * time.Hour)
			if err := upsertFactBatch(ctx, store, []facts.Envelope{replay}); err != nil {
				t.Fatalf("upsertFactBatch(replay) error = %v, want nil", err)
			}
			if got := readUnchangedSkipRow(t, ctx, db, factID); got.xmin != before.xmin {
				t.Fatalf("equal replay after %q change rewrote the row: xmin %s -> %s", name, before.xmin, got.xmin)
			}
		})
	}
}
