// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"
)

func TestContentFilesRelativePathIndexSkipsIndependentMigration125Live(t *testing.T) {
	ctx, database := openContentSearchIndexLiveDB(t)
	exec := SQLDB{DB: database}
	index, lifecycle, preceding := contentFilesRelativePathIndexMigrations(t)

	var independent Definition
	preIndex := make([]Definition, 0, len(preceding))
	for _, definition := range preceding {
		if definition.Name == "shared_projection_acceptance_generation_key" {
			independent = definition
			continue
		}
		preIndex = append(preIndex, definition)
	}
	if independent.Name == "" {
		t.Fatal("independent migration 125 not found")
	}
	if err := applyBootstrapDefinitionsWith(ctx, exec, preIndex, slog.Default(), schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("apply schema through 124: %v", err)
	}
	assertContentSearchIndexState(t, database, "ready")
	assertMigrationReceipt(t, ctx, database, independent, false)

	if err := applyBootstrapDefinitionsWith(ctx, exec, []Definition{index}, slog.Default(), schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("apply only migration 126: %v", err)
	}
	assertContentFilesRelativePathIndexDefinition(t, ctx, database)
	if err := applyBootstrapDefinitionsWith(ctx, exec, []Definition{lifecycle}, slog.Default(), schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("apply only migration 127: %v", err)
	}
	assertContentSearchIndexState(t, database, "ready")
	assertMigrationReceipt(t, ctx, database, independent, false)
	indexAppliedAt := assertMigrationReceipt(t, ctx, database, index, true)
	lifecycleAppliedAt := assertMigrationReceipt(t, ctx, database, lifecycle, true)

	for _, definition := range []Definition{index, lifecycle} {
		if err := applyBootstrapDefinitionsWith(ctx, exec, []Definition{definition}, slog.Default(), schemaBootstrapCoordination{}); err != nil {
			t.Fatalf("reapply migration %s: %v", definition.Name, err)
		}
	}
	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("normal bootstrap catches up migration 125: %v", err)
	}
	assertMigrationReceipt(t, ctx, database, independent, true)
	if got := assertMigrationReceipt(t, ctx, database, index, true); !got.Equal(indexAppliedAt) {
		t.Fatalf("migration 126 reapplied: first=%s later=%s", indexAppliedAt, got)
	}
	if got := assertMigrationReceipt(t, ctx, database, lifecycle, true); !got.Equal(lifecycleAppliedAt) {
		t.Fatalf("migration 127 reapplied: first=%s later=%s", lifecycleAppliedAt, got)
	}
	assertContentSearchIndexState(t, database, "ready")
}

func assertMigrationReceipt(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	definition Definition,
	want bool,
) time.Time {
	t.Helper()
	var appliedAt time.Time
	err := database.QueryRowContext(ctx, `
SELECT applied_at
FROM eshu_schema_migrations
WHERE path = $1 AND variant = 'full' AND checksum_sha256 = $2`,
		definition.Path, migrationChecksum(definition.SQL),
	).Scan(&appliedAt)
	if !want && err == sql.ErrNoRows {
		return time.Time{}
	}
	if err != nil {
		t.Fatalf("migration %s receipt error = %v, want present=%t", definition.Name, err, want)
	}
	if !want {
		t.Fatalf("migration %s receipt present, want absent", definition.Name)
	}
	return appliedAt
}
