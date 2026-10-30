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

// repoDependencyProofDisposableEnv is the explicit opt-in behind creating
// the repo-dependency proofs' disposable databases.
const repoDependencyProofDisposableEnv = "ESHU_REPO_DEPENDENCY_QUARANTINE_PROOF_DISPOSABLE"

// openRepoDependencyProofAdmin resolves the administrative DSN and
// disposable opt-in the repo-dependency proofs create their disposable
// databases through, or skips. The runner's
// ESHU_REPO_DEPENDENCY_QUARANTINE_PROOF_DSN/_DISPOSABLE pair wins; local
// runs fall back to the generic ESHU_POSTGRES_TEST_DSN/_DISPOSABLE pair. A
// family DSN set without its explicit opt-in fails closed; an unset pair
// skips, never fails.
func openRepoDependencyProofAdmin(t *testing.T) (adminDSN, optIn string) {
	t.Helper()
	if dsn := strings.TrimSpace(os.Getenv(repoDependencyQuarantineProofDSNEnv)); dsn != "" {
		if optIn = os.Getenv(repoDependencyProofDisposableEnv); optIn != "1" {
			t.Fatalf("%s is set without %s=1", repoDependencyQuarantineProofDSNEnv, repoDependencyProofDisposableEnv)
		}
		adminDSN, err := postgresproof.WithDatabase(dsn, "postgres")
		if err != nil {
			t.Fatalf("retarget quarantine proof DSN: %v", err)
		}
		return adminDSN, optIn
	}
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN")),
		os.Getenv("ESHU_POSTGRES_DISPOSABLE")
}

// openRepoDependencyProofTarget rewrites adminDSN to the database database
// is connected to, for handoff to a helper subprocess that must open the
// same disposable database directly.
func openRepoDependencyProofTarget(t *testing.T, adminDSN string, database *sql.DB) string {
	t.Helper()
	var name string
	if err := database.QueryRow("SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("read proof database: %v", err)
	}
	target, err := postgresproof.WithDatabase(adminDSN, name)
	if err != nil {
		t.Fatalf("target quarantine proof database: %v", err)
	}
	return target
}

// openRepoDependencyQuarantineProofDB bootstraps a fresh disposable database
// on the public schema and returns it with its drop. The database is
// dropped when the test ends. Replay coordinates and acceptance fences
// need the ingestion tables, so the full production bootstrap applies —
// including the lane's public-only 241a/241b authority migrations, which an
// isolated schema cannot host (the later 242a definition then fails with a
// missing eshu_security schema).
func openRepoDependencyQuarantineProofDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	adminDSN, optIn := openRepoDependencyProofAdmin(t)
	if adminDSN == "" {
		t.Skipf("set %s and %s=1 (or the ESHU_POSTGRES_TEST_DSN/_DISPOSABLE pair) to run the real-Postgres quarantine proof",
			repoDependencyQuarantineProofDSNEnv, repoDependencyProofDisposableEnv)
	}
	_, database := postgresproof.OpenDisposableBootstrapDatabase(t, adminDSN, optIn, 5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: database})
		})
	return database, func() { _ = database.Close() }
}
