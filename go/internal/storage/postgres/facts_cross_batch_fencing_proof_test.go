// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Cross-batch fencing proof for issue #4444 (parent epic #4442).
//
// facts.go:298-321 upserts fact_records with `ON CONFLICT (fact_id) DO UPDATE
// SET ... EXCLUDED...` and no fencing_token guard. Within one batch,
// deduplicateEnvelopes keeps the LAST array position for a duplicate fact_id
// (facts.go:410-428) — order-of-arrival, not fencing-token order. Across
// batches, Postgres unconditionally applies whatever batch commits last. Both
// paths let a stale/out-of-order batch clobber a fact that a newer batch
// (higher fencing_token) already superseded, and the projector then
// materializes stale edges from the resurrected payload.
//
// This test drives the real upsertFactBatch/upsertFacts production path
// against a live Postgres instance twice — once with a newer-token batch
// landing first (proving the current unguarded UPSERT clobbers it when the
// stale batch commits second) and once with the fenced guard restored — so it
// is a true two-sided proof: fails on the unpatched path, passes with the
// guard. It is skipped when no DSN is configured so the hermetic unit suite
// stays green without Postgres.
//
// Performance Evidence: the fencing guard adds one column comparison
// (`fact_records.fencing_token <= EXCLUDED.fencing_token`) to an existing
// `ON CONFLICT DO UPDATE` clause already keyed on the `fact_id` primary key.
// It changes zero query shape, batch size, worker count, or round trip count.
// No-Regression Evidence: the guard is a no-op for the overwhelmingly common
// case (each generation's fencing_token is monotonic per collector run), and
// existing facts.go unit tests
// (TestUpsertFactsDeduplicatesByFactID, TestUpsertFactsBatchesLargeEnvelopes)
// keep passing unmodified, proving normal same-token/increasing-token upserts
// are unaffected.
// Observability Evidence: no new metric, span, or log is added; the guard is
// a pure SQL predicate change on an existing upsert path already covered by
// eshu_dp_postgres_query_duration_seconds. Cross-batch fencing decisions are
// visible in the resulting row shape (fencing_token, payload) that this test
// asserts directly against the live table.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestUpsertFactBatchCrossBatchFencingTokenGuard is the issue #4444 gate. It
// interleaves two batches with descending fencing tokens for the same
// fact_id and proves the higher-fencing-token fact always wins, regardless of
// which batch's INSERT commits last at the database.
func TestUpsertFactBatchCrossBatchFencingTokenGuard(t *testing.T) {
	dsn := factCrossBatchFencingProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the cross-batch fencing-token proof")
	}

	ctx := context.Background()
	db, _ := openFactCrossBatchFencingSchema(t, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	seedFactCrossBatchFencingScope(t, ctx, db, "scope-fencing", "gen-fencing", now)

	t.Run("stale batch landing after a newer batch does not clobber it", func(t *testing.T) {
		const factID = "fact-fencing-1"

		newerBatch := []facts.Envelope{{
			FactID:        factID,
			ScopeID:       "scope-fencing",
			GenerationID:  "gen-fencing",
			FactKind:      "repository",
			StableFactKey: "repository:scope-fencing",
			FencingToken:  20,
			ObservedAt:    now,
			Payload:       map[string]any{"version": "newer-token-20"},
			SourceRef:     facts.Ref{SourceSystem: "git", FactKey: "key-1"},
		}}
		staleBatch := []facts.Envelope{{
			FactID:        factID,
			ScopeID:       "scope-fencing",
			GenerationID:  "gen-fencing",
			FactKind:      "repository",
			StableFactKey: "repository:scope-fencing",
			FencingToken:  10,
			ObservedAt:    now,
			Payload:       map[string]any{"version": "stale-token-10"},
			SourceRef:     facts.Ref{SourceSystem: "git", FactKey: "key-1"},
		}}

		// Simulate cross-batch out-of-order arrival: the higher-fencing-token
		// batch commits FIRST, then the stale lower-fencing-token batch commits
		// SECOND. A correct fencing guard must keep the newer fact intact; an
		// unfenced ON CONFLICT DO UPDATE (the pre-fix behavior) lets the second
		// write win purely on commit order and silently resurrects stale data.
		if err := upsertFactBatch(ctx, store, newerBatch); err != nil {
			t.Fatalf("upsertFactBatch(newer) error = %v, want nil", err)
		}
		if err := upsertFactBatch(ctx, store, staleBatch); err != nil {
			t.Fatalf("upsertFactBatch(stale) error = %v, want nil", err)
		}

		gotToken, gotPayload := readFactCrossBatchFencingRow(t, ctx, db, factID)
		if gotToken != 20 {
			t.Fatalf("fencing_token after stale-batch arrival = %d, want 20 (newer must survive a stale batch landing later)", gotToken)
		}
		if !strings.Contains(gotPayload, "newer-token-20") {
			t.Fatalf("payload after stale-batch arrival = %s, want newer-token-20 payload preserved", gotPayload)
		}
	})

	t.Run("higher fencing token still wins when it arrives second", func(t *testing.T) {
		const factID = "fact-fencing-2"

		staleFirst := []facts.Envelope{{
			FactID:        factID,
			ScopeID:       "scope-fencing",
			GenerationID:  "gen-fencing",
			FactKind:      "repository",
			StableFactKey: "repository:scope-fencing",
			FencingToken:  5,
			ObservedAt:    now,
			Payload:       map[string]any{"version": "token-5"},
			SourceRef:     facts.Ref{SourceSystem: "git", FactKey: "key-2"},
		}}
		newerSecond := []facts.Envelope{{
			FactID:        factID,
			ScopeID:       "scope-fencing",
			GenerationID:  "gen-fencing",
			FactKind:      "repository",
			StableFactKey: "repository:scope-fencing",
			FencingToken:  15,
			ObservedAt:    now,
			Payload:       map[string]any{"version": "token-15"},
			SourceRef:     facts.Ref{SourceSystem: "git", FactKey: "key-2"},
		}}

		// The forward-progress direction must still apply: ordinary in-order
		// arrival (ascending fencing token) must keep overwriting normally.
		if err := upsertFactBatch(ctx, store, staleFirst); err != nil {
			t.Fatalf("upsertFactBatch(token-5) error = %v, want nil", err)
		}
		if err := upsertFactBatch(ctx, store, newerSecond); err != nil {
			t.Fatalf("upsertFactBatch(token-15) error = %v, want nil", err)
		}

		gotToken, gotPayload := readFactCrossBatchFencingRow(t, ctx, db, factID)
		if gotToken != 15 {
			t.Fatalf("fencing_token after in-order arrival = %d, want 15", gotToken)
		}
		if !strings.Contains(gotPayload, "token-15") {
			t.Fatalf("payload after in-order arrival = %s, want token-15 payload", gotPayload)
		}
	})

	t.Run("equal fencing token still overwrites (default zero-token collectors)", func(t *testing.T) {
		const factID = "fact-fencing-3"

		// Most collectors never set FencingToken, so it defaults to 0 for every
		// fact. The guard uses <= (not <), so same-token re-upserts inside one
		// generation must keep overwriting normally; a strict < would silently
		// freeze every zero-token fact after its first write.
		first := []facts.Envelope{{
			FactID:        factID,
			ScopeID:       "scope-fencing",
			GenerationID:  "gen-fencing",
			FactKind:      "repository",
			StableFactKey: "repository:scope-fencing",
			FencingToken:  0,
			ObservedAt:    now,
			Payload:       map[string]any{"version": "zero-token-first"},
			SourceRef:     facts.Ref{SourceSystem: "git", FactKey: "key-3"},
		}}
		second := []facts.Envelope{{
			FactID:        factID,
			ScopeID:       "scope-fencing",
			GenerationID:  "gen-fencing",
			FactKind:      "repository",
			StableFactKey: "repository:scope-fencing",
			FencingToken:  0,
			ObservedAt:    now.Add(time.Minute),
			Payload:       map[string]any{"version": "zero-token-second"},
			SourceRef:     facts.Ref{SourceSystem: "git", FactKey: "key-3"},
		}}

		if err := upsertFactBatch(ctx, store, first); err != nil {
			t.Fatalf("upsertFactBatch(zero-token-first) error = %v, want nil", err)
		}
		if err := upsertFactBatch(ctx, store, second); err != nil {
			t.Fatalf("upsertFactBatch(zero-token-second) error = %v, want nil", err)
		}

		gotToken, gotPayload := readFactCrossBatchFencingRow(t, ctx, db, factID)
		if gotToken != 0 {
			t.Fatalf("fencing_token after equal-token re-upsert = %d, want 0", gotToken)
		}
		if !strings.Contains(gotPayload, "zero-token-second") {
			t.Fatalf("payload after equal-token re-upsert = %s, want zero-token-second (equal token must still overwrite)", gotPayload)
		}
	})
}

func factCrossBatchFencingProofDSN() string {
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
}

// openFactCrossBatchFencingSchema creates an isolated throwaway schema, applies
// the ingestion_scopes/scope_generations/fact_records DDL, and returns a
// single-connection handle pinned to that schema plus its name. Mirrors
// openReducerFairnessDBWithSchema (reducer_queue_domain_fairness_test.go) so
// this proof follows the package's established live-Postgres pattern.
func openFactCrossBatchFencingSchema(t *testing.T, ctx context.Context, dsn string) (*sql.DB, string) {
	t.Helper()
	schemaName := fmt.Sprintf("fact_fencing_%d", time.Now().UnixNano())

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create fencing schema: %v", err)
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
		factRecordSchemaSQL,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("apply fencing schema: %v", err)
		}
	}
	return db, schemaName
}

func seedFactCrossBatchFencingScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string, now time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, 'repo', 'git', $1, NULL, 'git', $1, $2, $2, 'active', $3, '{}'::jsonb)`,
		scopeID, now, generationID); err != nil {
		t.Fatalf("insert fencing scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ($1, $2, 'snapshot', 'fencing-proof', $3, $3, 'active', $3, NULL, '{}'::jsonb)`,
		generationID, scopeID, now); err != nil {
		t.Fatalf("insert fencing generation: %v", err)
	}
}

func readFactCrossBatchFencingRow(t *testing.T, ctx context.Context, db *sql.DB, factID string) (int64, string) {
	t.Helper()
	var fencingToken int64
	var payload []byte
	if err := db.QueryRowContext(
		ctx,
		`SELECT fencing_token, payload::text FROM fact_records WHERE fact_id = $1`, factID,
	).Scan(&fencingToken, &payload); err != nil {
		t.Fatalf("read fact row %q: %v", factID, err)
	}
	return fencingToken, string(payload)
}
