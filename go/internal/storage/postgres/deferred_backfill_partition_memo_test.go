// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDeferredBackfillPartitionMemoSchemaSQLMirrorsMigrationFile keeps the Go
// DDL constant (used directly by tests and EnsureSchema-style callers) in
// lockstep with the embedded migration 042_deferred_backfill_partition_memo.sql
// that ApplyBootstrap actually runs — the same convention
// TestBootstrapSQLFilesMirrorDefinitions enforces for every other migration.
func TestDeferredBackfillPartitionMemoSchemaSQLMirrorsMigrationFile(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	path := filepath.Join(repoRoot, "go", "internal", "storage", "postgres",
		"migrations", "042_deferred_backfill_partition_memo.sql")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(DeferredBackfillPartitionMemoSchemaSQL()) {
		t.Fatalf("migration file %q does not match DeferredBackfillPartitionMemoSchemaSQL()", path)
	}
}

// noopExecQueryer satisfies ExecQueryer without a real database, for the
// zero-row short-circuit paths below that must never issue a query. Any call
// into ExecContext/QueryContext fails the test loudly instead of silently
// no-oping, so a regression that starts issuing a query on an empty input is
// caught.
type noopExecQueryer struct{ t *testing.T }

func (n noopExecQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	n.t.Helper()
	n.t.Fatal("ExecContext must not be called for a zero-row batch")
	return nil, nil
}

func (n noopExecQueryer) QueryContext(context.Context, string, ...any) (Rows, error) {
	n.t.Helper()
	n.t.Fatal("QueryContext must not be called for an empty partition list")
	return nil, nil
}

func TestUpsertDeferredBackfillPartitionMemoBatchRejectsBlankIdentity(t *testing.T) {
	t.Parallel()

	err := upsertDeferredBackfillPartitionMemoBatch(context.Background(), noopExecQueryer{t: t}, []deferredBackfillPartitionMemoRow{
		{ScopeID: "  ", GenerationID: "gen-1", CatalogFingerprint: "sha256:x", CommittedAt: time.Now()},
	})
	if err == nil {
		t.Fatal("expected error for blank scope_id, got nil")
	}
}

func TestUpsertDeferredBackfillPartitionMemoBatchRejectsBlankGeneration(t *testing.T) {
	t.Parallel()

	err := upsertDeferredBackfillPartitionMemoBatch(context.Background(), noopExecQueryer{t: t}, []deferredBackfillPartitionMemoRow{
		{ScopeID: "scope-1", GenerationID: " ", CatalogFingerprint: "sha256:x", CommittedAt: time.Now()},
	})
	if err == nil {
		t.Fatal("expected error for blank generation_id, got nil")
	}
}

func TestDeferredBackfillPartitionMemoStoreUpsertNoRowsIsNoop(t *testing.T) {
	t.Parallel()

	store := newDeferredBackfillPartitionMemoStore(noopExecQueryer{t: t})
	if err := store.Upsert(context.Background(), nil); err != nil {
		t.Fatalf("Upsert(nil) error = %v, want nil", err)
	}
}

func TestDeferredBackfillPartitionMemoStoreLookupManyEmptyInputIsNoop(t *testing.T) {
	t.Parallel()

	store := newDeferredBackfillPartitionMemoStore(noopExecQueryer{t: t})
	got, err := store.LookupMany(context.Background(), nil)
	if err != nil {
		t.Fatalf("LookupMany(nil) error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("LookupMany(nil) = %v, want nil map", got)
	}
}
