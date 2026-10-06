// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TrigramExtensionLockKey is the transaction advisory-lock key that serializes
// the pg_trgm install on a shared proof server. The storage/postgres live
// helpers use the same key, so their installs and this package's never race.
const TrigramExtensionLockKey int64 = 7260006809

// DeferredPartitionProofDSN returns the DSN of the disposable PostgreSQL the
// deferred-partition and activation-obligation live proofs share:
// ESHU_DEFERRED_PARTITION_PROOF_DSN, else ESHU_LATEST_GENERATION_PROOF_DSN. It
// skips the test when neither is set.
func DeferredPartitionProofDSN(t testing.TB) string {
	t.Helper()
	if dsn := os.Getenv("ESHU_DEFERRED_PARTITION_PROOF_DSN"); dsn != "" {
		return dsn
	}
	if dsn := os.Getenv("ESHU_LATEST_GENERATION_PROOF_DSN"); dsn != "" {
		return dsn
	}
	t.Skip("set ESHU_DEFERRED_PARTITION_PROOF_DSN (or ESHU_LATEST_GENERATION_PROOF_DSN) to run the deferred backfill partition-memo Postgres proof")
	return ""
}

// InstallTrigramExtension installs pg_trgm in the public schema under
// TrigramExtensionLockKey. A bootstrap's own CREATE EXTENSION would otherwise
// land in the first schema on the search_path and vanish with that schema's
// DROP, and IF NOT EXISTS alone lets two sessions race on the extension's
// unique index; the advisory lock and the CREATE share one transaction, so
// installs run one at a time.
func InstallTrigramExtension(ctx context.Context, t testing.TB, admin *sql.DB) {
	t.Helper()
	tx, err := admin.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin pg_trgm install: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", TrigramExtensionLockKey); err != nil {
		t.Fatalf("lock pg_trgm install: %v", err)
	}
	if _, err := tx.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public"); err != nil {
		t.Fatalf("install pg_trgm in public: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit pg_trgm install: %v", err)
	}
}

// OpenIsolatedSchema creates a fresh schema named prefix_<unix nanos> in the
// database dsn names, runs apply on a pool whose every connection resolves
// unqualified names in that schema first, and returns the pool. The schema is
// dropped when the test ends. public stays on the search_path so pg_trgm,
// installed there first by InstallTrigramExtension, still resolves its
// operator classes for the schema apply creates. A live proof that drives a
// queue claim or an all-scopes recovery needs its own schema, or it acts on
// rows other tests left behind. Unlike OpenDisposableDatabase it does not
// validate dsn or require an opt-in, and its connections do not run the infra
// writer SET; point it only at a disposable server.
func OpenIsolatedSchema(t testing.TB, dsn, prefix string, apply func(context.Context, *sql.DB) error) *sql.DB {
	t.Helper()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	admin.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = admin.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	InstallTrigramExtension(ctx, t, admin)
	schema := isolatedSchemaName(prefix)
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+quoted+" CASCADE"); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})

	schemaDSN, err := isolatedSchemaDSN(dsn, schema)
	if err != nil {
		t.Fatalf("parse Postgres DSN: %v", err)
	}
	database, err := sql.Open("pgx", schemaDSN)
	if err != nil {
		t.Fatalf("open isolated schema: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := apply(ctx, database); err != nil {
		t.Fatalf("apply schema to isolated schema: %v", err)
	}
	return database
}

func isolatedSchemaName(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func isolatedSchemaDSN(dsn, schema string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse proof DSN: %w", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
