// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package offlinetier_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// trigramSharingProofDSNEnv names the disposable PostgreSQL database the
// trigram-sharing proof bootstraps its sibling schemas in. The fallback chain
// mirrors the repo-dependency proofs with test-local names, so this proof
// never depends on the #9299-lane helpers those names live with.
const trigramSharingProofDSNEnv = "ESHU_REPO_DEPENDENCY_QUARANTINE_PROOF_DSN"

func firstTrigramSharingProofDSN() string {
	for _, name := range []string{trigramSharingProofDSNEnv, "ESHU_SHARED_PROJECTION_RESCALE_PROOF_DSN", "ESHU_POSTGRES_DSN"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

// TestRepoDependencyProofSchemasShareTrigramLive is the #9572 regression proof.
// Two sibling isolated schemas bootstrap in one shared database through the
// shared OpenIsolatedSchema helper, which pins pg_trgm to public before the
// first CREATE SCHEMA: without the shared install, the second bootstrap fails
// at migration 004 with `operator class "gin_trgm_ops" does not exist`
// (SQLSTATE 42704). The test keeps both schemas open at once, covering
// coexisting siblings, and then proves each schema actually resolved the
// opclass by reading back the validity of the two trigram indexes migration
// 004 builds. It needs only PostgreSQL: no graph backend, no Odù fixture.
func TestRepoDependencyProofSchemasShareTrigramLive(t *testing.T) {
	dsn := firstTrigramSharingProofDSN()
	if dsn == "" {
		t.Skipf("set %s or ESHU_POSTGRES_DSN to run the trigram-sharing proof", trigramSharingProofDSNEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	apply := func(ctx context.Context, db *sql.DB) error {
		return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: db})
	}
	quarantineDB := postgresproof.OpenIsolatedSchema(t, dsn, "repo_dependency_trigram_quarantine", apply)
	processDeathDB := postgresproof.OpenIsolatedSchema(t, dsn, "repo_dependency_trigram_process_death", apply)
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
