// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package offlinetier_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

func openRepoDependencyQuarantineProofDB(ctx context.Context, t *testing.T, dsn string) (*sql.DB, func()) {
	t.Helper()
	bootstrapDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open quarantine proof bootstrap database: %v", err)
	}
	// Pin pg_trgm to public before creating the schema (#9572): the
	// bootstrap's unqualified CREATE EXTENSION would otherwise install the
	// opclass into this private schema, hiding it from every sibling schema
	// bootstrapped later in the same database.
	postgresproof.InstallTrigramExtension(ctx, t, bootstrapDB)
	schemaName := fmt.Sprintf("repo_dependency_odu_quarantine_%d", time.Now().UnixNano())
	if _, err := bootstrapDB.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		_ = bootstrapDB.Close()
		t.Fatalf("create quarantine proof schema: %v", err)
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	// public stays on the path so the pg_trgm operator classes pinned above
	// resolve for this schema's bootstrap.
	database, err := sql.Open("pgx", dsn+separator+"search_path="+schemaName+",public")
	if err != nil {
		_ = bootstrapDB.Close()
		t.Fatalf("open scoped quarantine proof database: %v", err)
	}
	// Replay coordinates and acceptance fences need the ingestion tables too.
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database}); err != nil {
		_ = database.Close()
		_ = bootstrapDB.Close()
		t.Fatalf("ensure quarantine proof schema: %v", err)
	}
	return database, func() {
		_ = database.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = bootstrapDB.ExecContext(cleanupCtx, "DROP SCHEMA "+schemaName+" CASCADE")
		_ = bootstrapDB.Close()
	}
}
