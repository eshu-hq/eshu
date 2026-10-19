// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package poison_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// The proof schema derives from the production bootstrap
// (postgres.ApplyBootstrapWithoutContentSearchIndexes, #8302): the previous
// hand-built table set silently fell behind the production schema on every
// migration the recovery statement depends on
// (container_image_identity_v2_required, ...), so the bounded-arm and
// write-time race proofs failed with a missing column far from the real
// cause. A full isolated bootstrap costs about two seconds; deriving from it
// keeps the proof schema pinned to production by construction, so no
// drift-guard test is needed — there is no hand-picked list left to drift.

// openPoisonLivenessProofDB opens a Postgres connection for a poison-liveness
// integration proof run. Single-threaded (MaxOpenConns=1) to avoid
// cross-connection search_path confusion, mirroring openLivenessProofDB.
func openPoisonLivenessProofDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// provisionPoisonLivenessSchema creates a fresh isolated proof schema, applies
// the production bootstrap schema, and seeds it. The schema is dropped via
// t.Cleanup. public stays on the search_path so the pg_trgm operator classes
// the bootstrap names still resolve, mirroring openReducerFairnessSchemaConn
// (#7489).
func provisionPoisonLivenessSchema(t *testing.T, db *sql.DB, seedSQL string) {
	t.Helper()
	ctx := context.Background()
	schemaName := fmt.Sprintf("poison_liveness_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName+",public"); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if err := postgres.ApplyBootstrapWithoutContentSearchIndexes(ctx, postgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply proof schema: %v", err)
	}
	if seedSQL != "" {
		if _, err := db.ExecContext(ctx, seedSQL); err != nil {
			t.Fatalf("seed proof data: %v", err)
		}
	}
}
