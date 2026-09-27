// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build issue7033_rollout

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

const (
	issue7033LegacyIndexMigrationPathForTest     = "go/internal/storage/postgres/migrations/130_content_files_relative_path_trgm_index.sql"
	issue7033LegacyIndexMigrationChecksumForTest = "ef395de0a2c1ad86fcbc1f82abcda08c83e689508a1197d6e2848695978a8e41"
	issue7033LegacyLifecycleMigrationPathForTest = "go/internal/storage/postgres/migrations/131_content_files_relative_path_trgm_index_lifecycle.sql"
	issue7033LegacyLifecycleMigrationSumForTest  = "c0494bb1489ca3900f62675aacf0b37cd5524e868ba96a124640f6142b14ef2b"
)

func TestIssue7033ScopedRolloutAdoptsExactLegacy130And131ReceiptsLive(t *testing.T) {
	ctx, database, definitions := issue7033ScopedRolloutWith125Through129DeferredLive(t)
	seedIssue7033LegacyPathIndex(t, ctx, database)
	legacyIndexReceipt := issue7033LegacyReceipt(t, ctx, database, issue7033LegacyIndexMigrationPathForTest)
	legacyLifecycleReceipt := issue7033LegacyReceipt(t, ctx, database, issue7033LegacyLifecycleMigrationPathForTest)
	indexOID, indexRelFileNode := issue7033PathIndexIdentity(t, ctx, database)

	if err := runIssue7033ScopedRollout(ctx, database, issue7033RolloutTargetConfig(t, ctx, database), issue7033TestLogger()); err != nil {
		t.Fatalf("runIssue7033ScopedRollout() legacy adoption error = %v", err)
	}
	for _, definition := range definitions {
		assertMigrationReceipt(t, ctx, database, definition, true)
	}
	if got := issue7033LegacyReceipt(t, ctx, database, issue7033LegacyIndexMigrationPathForTest); !got.Equal(legacyIndexReceipt) {
		t.Fatalf("legacy 130 receipt changed: before=%s after=%s", legacyIndexReceipt, got)
	}
	if got := issue7033LegacyReceipt(t, ctx, database, issue7033LegacyLifecycleMigrationPathForTest); !got.Equal(legacyLifecycleReceipt) {
		t.Fatalf("legacy 131 receipt changed: before=%s after=%s", legacyLifecycleReceipt, got)
	}
	if gotOID, gotRelFileNode := issue7033PathIndexIdentity(t, ctx, database); gotOID != indexOID || gotRelFileNode != indexRelFileNode {
		t.Fatalf("legacy path index changed: before oid=%d relfilenode=%d after oid=%d relfilenode=%d", indexOID, indexRelFileNode, gotOID, gotRelFileNode)
	}
	assertContentSearchIndexState(t, database, "ready")
}

func TestIssue7033ScopedRolloutRejectsUnprovenLegacyPathIndexBeforeDDL(t *testing.T) {
	for _, tc := range []struct {
		name     string
		seed     func(*testing.T, context.Context, *sql.DB)
		want     string
		receipts [2]bool
	}{
		{
			name: "missing_legacy_index_receipt",
			seed: func(t *testing.T, ctx context.Context, database *sql.DB) {
				seedIssue7033LegacyPathIndexOnly(t, ctx, database)
				issue7033InsertLegacyReceipt(t, ctx, database, issue7033LegacyLifecycleMigrationPathForTest, issue7033LegacyLifecycleMigrationSumForTest)
			},
			want: "untracked relative-path index",
		},
		{
			name: "wrong_legacy_index_checksum",
			seed: func(t *testing.T, ctx context.Context, database *sql.DB) {
				seedIssue7033LegacyPathIndexOnly(t, ctx, database)
				issue7033InsertLegacyReceipt(t, ctx, database, issue7033LegacyIndexMigrationPathForTest, strings.Repeat("0", 64))
				issue7033InsertLegacyReceipt(t, ctx, database, issue7033LegacyLifecycleMigrationPathForTest, issue7033LegacyLifecycleMigrationSumForTest)
			},
			want: "legacy migration 130 checksum does not match expected receipt",
		},
		{
			name: "missing_legacy_lifecycle_receipt",
			seed: func(t *testing.T, ctx context.Context, database *sql.DB) {
				seedIssue7033LegacyPathIndexOnly(t, ctx, database)
				issue7033InsertLegacyReceipt(t, ctx, database, issue7033LegacyIndexMigrationPathForTest, issue7033LegacyIndexMigrationChecksumForTest)
			},
			want: "untracked relative-path index",
		},
		{
			name: "wrong_legacy_lifecycle_checksum",
			seed: func(t *testing.T, ctx context.Context, database *sql.DB) {
				seedIssue7033LegacyPathIndexOnly(t, ctx, database)
				issue7033InsertLegacyReceipt(t, ctx, database, issue7033LegacyIndexMigrationPathForTest, issue7033LegacyIndexMigrationChecksumForTest)
				issue7033InsertLegacyReceipt(t, ctx, database, issue7033LegacyLifecycleMigrationPathForTest, strings.Repeat("0", 64))
			},
			want: "legacy migration 131 checksum does not match expected receipt",
		},
		{
			name: "malformed_legacy_index",
			seed: func(t *testing.T, ctx context.Context, database *sql.DB) {
				if _, err := database.ExecContext(ctx, "CREATE INDEX content_files_relative_path_trgm_idx ON content_files (relative_path)"); err != nil {
					t.Fatalf("seed malformed legacy path index: %v", err)
				}
				issue7033InsertLegacyReceipts(t, ctx, database)
			},
			want: "not the exact valid ready live gin_trgm_ops definition",
		},
		{
			name: "new_lifecycle_without_new_index",
			seed: func(t *testing.T, ctx context.Context, database *sql.DB) {
				seedIssue7033LegacyPathIndex(t, ctx, database)
				issue7033InsertMigrationReceipt(t, ctx, database, definitionsForIssue7033Rollout(t)[1])
			},
			want:     "migration 135 receipt exists without migration 134 receipt",
			receipts: [2]bool{false, true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, database, definitions := issue7033ScopedRolloutWith125Through129DeferredLive(t)
			tc.seed(t, ctx, database)

			err := runIssue7033ScopedRollout(ctx, database, issue7033RolloutTargetConfig(t, ctx, database), issue7033TestLogger())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("runIssue7033ScopedRollout() error = %v, want %q", err, tc.want)
			}
			for index, definition := range definitions {
				assertMigrationReceipt(t, ctx, database, definition, tc.receipts[index])
			}
		})
	}
}

func TestIssue7033ScopedRolloutRetriesAfter134ReceiptLive(t *testing.T) {
	ctx, database, definitions := issue7033ScopedRolloutWith125Through129DeferredLive(t)
	if err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, definitions[:1], issue7033TestLogger(), schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("apply migration 134 before retry: %v", err)
	}
	assertMigrationReceipt(t, ctx, database, definitions[0], true)
	assertMigrationReceipt(t, ctx, database, definitions[1], false)

	if err := runIssue7033ScopedRollout(ctx, database, issue7033RolloutTargetConfig(t, ctx, database), issue7033TestLogger()); err != nil {
		t.Fatalf("runIssue7033ScopedRollout() after 134 receipt error = %v", err)
	}
	assertMigrationReceipt(t, ctx, database, definitions[0], true)
	assertMigrationReceipt(t, ctx, database, definitions[1], true)
}

func definitionsForIssue7033Rollout(t *testing.T) []Definition {
	t.Helper()
	definitions, err := issue7033ScopedRolloutDefinitions()
	if err != nil {
		t.Fatalf("issue7033ScopedRolloutDefinitions(): %v", err)
	}
	return definitions
}

func seedIssue7033LegacyPathIndex(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	seedIssue7033LegacyPathIndexOnly(t, ctx, database)
	issue7033InsertLegacyReceipts(t, ctx, database)
}

func seedIssue7033LegacyPathIndexOnly(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
CREATE INDEX content_files_relative_path_trgm_idx
    ON content_files USING gin (relative_path gin_trgm_ops)`); err != nil {
		t.Fatalf("seed exact legacy path index: %v", err)
	}
}

func issue7033InsertLegacyReceipts(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	issue7033InsertLegacyReceipt(t, ctx, database, issue7033LegacyIndexMigrationPathForTest, issue7033LegacyIndexMigrationChecksumForTest)
	issue7033InsertLegacyReceipt(t, ctx, database, issue7033LegacyLifecycleMigrationPathForTest, issue7033LegacyLifecycleMigrationSumForTest)
}

func issue7033InsertLegacyReceipt(t *testing.T, ctx context.Context, database *sql.DB, path, checksum string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO eshu_schema_migrations (path, variant, checksum_sha256)
VALUES ($1, 'full', $2)`, path, checksum); err != nil {
		t.Fatalf("seed legacy migration receipt %s: %v", path, err)
	}
}

func issue7033InsertMigrationReceipt(t *testing.T, ctx context.Context, database *sql.DB, definition Definition) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO eshu_schema_migrations (path, variant, checksum_sha256)
VALUES ($1, 'full', $2)`, definition.Path, migrationChecksum(definition.SQL)); err != nil {
		t.Fatalf("seed migration receipt %s: %v", definition.Name, err)
	}
}

func issue7033LegacyReceipt(t *testing.T, ctx context.Context, database *sql.DB, path string) time.Time {
	t.Helper()
	var appliedAt time.Time
	if err := database.QueryRowContext(ctx, `
SELECT applied_at FROM eshu_schema_migrations WHERE path = $1 AND variant = 'full'`, path).Scan(&appliedAt); err != nil {
		t.Fatalf("read legacy receipt %s: %v", path, err)
	}
	return appliedAt
}

func issue7033PathIndexIdentity(t *testing.T, ctx context.Context, database *sql.DB) (int64, int64) {
	t.Helper()
	var oid, relFileNode int64
	if err := database.QueryRowContext(ctx, `
SELECT index_relation.oid, index_relation.relfilenode
FROM pg_class AS index_relation
JOIN pg_namespace AS index_schema ON index_schema.oid = index_relation.relnamespace
WHERE index_schema.nspname = 'public'
  AND index_relation.relname = 'content_files_relative_path_trgm_idx'`).Scan(&oid, &relFileNode); err != nil {
		t.Fatalf("read path index identity: %v", err)
	}
	return oid, relFileNode
}
