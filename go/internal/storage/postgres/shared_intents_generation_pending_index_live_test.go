// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSharedIntentGenerationPendingIndexLifecycleLive proves that the
// liveness lookup index can be installed, replayed, and rolled back on data.
func TestSharedIntentGenerationPendingIndexLifecycleLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_GENERATION_LIVENESS_PROOF_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_GENERATION_LIVENESS_PROOF_DSN for the live index lifecycle proof")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	admin.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = admin.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	schema := fmt.Sprintf("eshu_6738_intent_index_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE"); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse Postgres DSN: %v", err)
	}
	params := parsed.Query()
	params.Set("search_path", schema)
	parsed.RawQuery = params.Encode()
	db, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("open isolated schema: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(ctx, `
CREATE TABLE shared_projection_intents (
	intent_id TEXT PRIMARY KEY,
	generation_id TEXT NOT NULL,
	completed_at TIMESTAMPTZ NULL
);
INSERT INTO shared_projection_intents (intent_id, generation_id, completed_at)
SELECT 'pending-' || i, 'generation-' || (i % 40), NULL
FROM generate_series(1, 2000) AS i;
INSERT INTO shared_projection_intents (intent_id, generation_id, completed_at)
SELECT 'done-' || i, 'generation-' || (i % 40), now()
FROM generate_series(1, 2000) AS i;
ANALYZE shared_projection_intents;
`); err != nil {
		t.Fatalf("seed populated intents: %v", err)
	}

	var definition Definition
	for _, candidate := range BootstrapDefinitions() {
		if candidate.Name == "shared_projection_generation_pending_index" {
			definition = candidate
			break
		}
	}
	if definition.Name == "" {
		t.Fatal("shared_projection_generation_pending_index migration missing")
	}
	for pass := 1; pass <= 2; pass++ {
		if err := ApplyDefinitions(ctx, SQLDB{DB: db}, []Definition{definition}); err != nil {
			t.Fatalf("apply migration pass %d: %v", pass, err)
		}
	}
	const indexName = "shared_projection_intents_generation_pending_idx"
	assertSharedIntentGenerationIndex(ctx, t, db, schema, indexName, true)

	restarted, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("restart isolated connection: %v", err)
	}
	restarted.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = restarted.Close() })
	if err := ApplyDefinitions(ctx, SQLDB{DB: restarted}, []Definition{definition}); err != nil {
		t.Fatalf("reapply after restart: %v", err)
	}
	assertSharedIntentGenerationIndex(ctx, t, restarted, schema, indexName, true)

	// Force an index candidate so this proof checks the query predicate, not
	// only index existence on a table where a sequential scan is cheap.
	if _, err := restarted.ExecContext(ctx, "SET enable_seqscan = off"); err != nil {
		t.Fatalf("disable sequential scan for plan proof: %v", err)
	}
	rows, err := restarted.QueryContext(ctx, `EXPLAIN SELECT 1 FROM shared_projection_intents
WHERE generation_id = 'generation-7' AND completed_at IS NULL LIMIT 1`)
	if err != nil {
		t.Fatalf("explain generation lookup: %v", err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			_ = rows.Close()
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		t.Fatalf("plan rows: %v", err)
	}
	_ = rows.Close()
	if !strings.Contains(plan.String(), indexName) {
		t.Fatalf("generation lookup did not use %s: %s", indexName, plan.String())
	}

	if _, err := restarted.ExecContext(ctx, "DROP INDEX CONCURRENTLY IF EXISTS "+indexName); err != nil {
		t.Fatalf("rollback index: %v", err)
	}
	assertSharedIntentGenerationIndex(ctx, t, restarted, schema, indexName, false)
	if err := ApplyDefinitions(ctx, SQLDB{DB: restarted}, []Definition{definition}); err != nil {
		t.Fatalf("reapply after rollback: %v", err)
	}
	assertSharedIntentGenerationIndex(ctx, t, restarted, schema, indexName, true)
}

func assertSharedIntentGenerationIndex(ctx context.Context, t *testing.T, db *sql.DB, schema, indexName string, wantPresent bool) {
	t.Helper()
	var valid, ready bool
	err := db.QueryRowContext(ctx, `
SELECT i.indisvalid, i.indisready
FROM pg_index AS i
JOIN pg_class AS c ON c.oid = i.indexrelid
JOIN pg_namespace AS n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relname = $2`, schema, indexName).Scan(&valid, &ready)
	if !wantPresent {
		if err != sql.ErrNoRows {
			t.Fatalf("index %s after rollback: err=%v, want no rows", indexName, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("inspect index %s: %v", indexName, err)
	}
	if !valid || !ready {
		t.Fatalf("index %s valid=%t ready=%t, want true/true", indexName, valid, ready)
	}
}
