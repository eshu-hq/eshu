// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"testing"
	"time"
)

// openIsolatedBootstrapSchema applies the real bootstrap migrations to a fresh
// schema of the database named by dsn and returns a pool whose every
// connection resolves unqualified names there. The schema is dropped when the
// test ends.
//
// A live proof that drives a queue Claim or an all-scopes recovery must use it
// (#7479). Both act on every matching row in the database, so on a shared,
// long-lived schema they pick up rows other tests or earlier runs left behind:
// the proof then claims, acks or reopens the wrong work item, and whether it
// passes depends on how much history the database holds.
//
// The isolated schema comes first on the search_path. public stays on it so
// the pg_trgm extension, installed there under the package's advisory lock,
// still resolves its operator classes for the bootstrap.
func openIsolatedBootstrapSchema(t *testing.T, dsn, prefix string) *sql.DB {
	t.Helper()

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	admin.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = admin.Close() })

	schemaCtx, cancelSchema := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelSchema()
	installGenerationRetentionTrigramExtension(schemaCtx, t, admin)
	schema := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	if _, err := admin.ExecContext(schemaCtx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE"); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse Postgres DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	database, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("open isolated schema: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ApplyBootstrap(schemaCtx, SQLDB{DB: database}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	return database
}
