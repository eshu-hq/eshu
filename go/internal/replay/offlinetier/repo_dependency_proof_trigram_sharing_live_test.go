// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package offlinetier_test

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestRepoDependencyProofSchemasShareTrigramLive is the #9572 regression proof.
// Both isolated-schema proof helpers bootstrap a fresh schema in one shared
// database, so whichever helper runs first must leave pg_trgm visible to the
// second: without the shared install, the second bootstrap fails at migration
// 004 with `operator class "gin_trgm_ops" does not exist` (SQLSTATE 42704).
// The test keeps both schemas open at once, covering coexisting siblings, and
// then proves each schema actually resolved the opclass by reading back the
// validity of the two trigram indexes migration 004 builds. It needs only
// PostgreSQL: no graph backend, no Odù fixture.
func TestRepoDependencyProofSchemasShareTrigramLive(t *testing.T) {
	dsn := firstRepoDependencyQuarantineDSN()
	if dsn == "" {
		t.Skipf("set %s or ESHU_POSTGRES_DSN to run the trigram-sharing proof", repoDependencyQuarantineProofDSNEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	quarantineDB, cleanupQuarantine := openRepoDependencyQuarantineProofDB(ctx, t, dsn)
	defer cleanupQuarantine()
	processDeathDB, _, cleanupProcessDeath := openRepoDependencyProcessDeathProofDB(ctx, t, dsn)
	defer cleanupProcessDeath()
	assertTrigramIndexesLive(t, ctx, quarantineDB, "quarantine")
	assertTrigramIndexesLive(t, ctx, processDeathDB, "process-death")
}

func assertTrigramIndexesLive(t *testing.T, ctx context.Context, db *sql.DB, name string) {
	t.Helper()
	for _, index := range []string{"content_files_content_trgm_idx", "content_entities_source_trgm_idx"} {
		var valid bool
		if err := db.QueryRowContext(ctx, `SELECT indisvalid FROM pg_index WHERE indexrelid = to_regclass($1)`, index).Scan(&valid); err != nil {
			t.Fatalf("%s schema: trigram index %s lookup: %v", name, index, err)
		}
		if !valid {
			t.Fatalf("%s schema: trigram index %s is invalid", name, index)
		}
	}
}
