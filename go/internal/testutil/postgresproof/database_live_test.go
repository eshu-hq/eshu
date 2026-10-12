// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestOpenDisposableDatabaseLiveMarksConnectionsAsInfraWriters pins that the
// disposable proof database connects the way the service runtimes do, with
// the derive-aware writer setting, so a proof that seeds infra-typed content
// rows cannot leave rolling-upgrade fence marks behind. Set
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN (an administrative postgres-database
// DSN) and ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 to run it.
func TestOpenDisposableDatabaseLiveMarksConnectionsAsInfraWriters(t *testing.T) {
	ctx, db := OpenDisposableDatabase(t,
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE"),
		time.Minute)
	var setting string
	if err := db.QueryRowContext(ctx,
		`SELECT coalesce(current_setting('eshu.infra_inventory_writer', true), '')`).Scan(&setting); err != nil {
		t.Fatalf("read session setting: %v", err)
	}
	if setting != "derive" {
		t.Fatalf("eshu.infra_inventory_writer = %q, want derive on disposable proof connections", setting)
	}
}

// TestOpenDisposableDatabaseLiveIsPristineFromTemplate0 plants pg_trgm and a
// canary table in template1, then proves the disposable database inherits
// neither. Creating from template1 would copy both: that is the shape PR
// #7938 observed, where a pg_trgm already installed in public hid gin_trgm_ops
// from an isolated schema. The plant is removed during cleanup, and a final
// cleanup asserts no proof database survives the helper's own drop. Set
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN (an administrative postgres-database
// DSN whose role may install extensions in template1) and
// ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 to run it.
func TestOpenDisposableDatabaseLiveIsPristineFromTemplate0(t *testing.T) {
	adminDSN := strings.TrimSpace(os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN"))
	optIn := os.Getenv("ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE")
	if adminDSN == "" || optIn == "" {
		t.Skip("set ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN and ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 to run the template0 isolation proof")
	}

	plantDB, err := sql.Open("pgx", disposableProofTemplate1DSN(t, adminDSN))
	if err != nil {
		t.Fatalf("open template1 for the isolation plant: %v", err)
	}
	plantCtx, plantCancel := context.WithTimeout(t.Context(), time.Minute)
	defer plantCancel()
	if err := plantDB.PingContext(plantCtx); err != nil {
		t.Fatalf("ping template1 for the isolation plant: %v", err)
	}
	if _, err := plantDB.ExecContext(plantCtx, "CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public"); err != nil {
		_ = plantDB.Close()
		if strings.Contains(err.Error(), "permission denied") {
			t.Skipf("template1 plant needs extension-install privilege: %v", err)
		}
		t.Fatalf("plant pg_trgm in template1: %v", err)
	}
	if _, err := plantDB.ExecContext(plantCtx, "CREATE TABLE IF NOT EXISTS public.eshu_proof_template_canary (id integer)"); err != nil {
		_ = plantDB.Close()
		t.Fatalf("plant canary table in template1: %v", err)
	}
	// Close before creating the proof database: any open connection to the
	// source template makes CREATE DATABASE fail with SQLSTATE 55006, which
	// would mask the inheritance assertion below. The restore cleanup opens
	// its own connection.
	if err := plantDB.Close(); err != nil {
		t.Fatalf("close template1 after the isolation plant: %v", err)
	}
	t.Cleanup(func() {
		restoreDB, err := sql.Open("pgx", disposableProofTemplate1DSN(t, adminDSN))
		if err != nil {
			t.Errorf("open template1 to remove the isolation plant: %v", err)
			return
		}
		defer func() { _ = restoreDB.Close() }()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
		defer cleanupCancel()
		if _, err := restoreDB.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS public.eshu_proof_template_canary"); err != nil {
			t.Errorf("remove template1 canary table: %v", err)
		}
		if _, err := restoreDB.ExecContext(cleanupCtx, "DROP EXTENSION IF EXISTS pg_trgm"); err != nil {
			t.Errorf("remove template1 pg_trgm plant: %v", err)
		}
	})
	// Registered before the helper call so it runs after the helper's own
	// drop. The name is read back from the connection, so the check names the
	// exact database this test created and cannot trip on a concurrent
	// proof in another package.
	var proofDBName string
	t.Cleanup(func() {
		if proofDBName == "" {
			return
		}
		assertProofDatabaseDropped(t, adminDSN, proofDBName)
	})

	ctx, db := OpenDisposableDatabase(t, adminDSN, optIn, time.Minute)
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&proofDBName); err != nil {
		t.Fatalf("read proof database name: %v", err)
	}
	var extensionCount, publicRelationCount int
	if err := db.QueryRowContext(ctx,
		`SELECT (SELECT count(*) FROM pg_extension WHERE extname <> 'plpgsql'),
                (SELECT count(*) FROM pg_class WHERE relnamespace = 'public'::regnamespace)`).Scan(&extensionCount, &publicRelationCount); err != nil {
		t.Fatalf("read disposable database isolation: %v", err)
	}
	if extensionCount != 0 || publicRelationCount != 0 {
		t.Fatalf("proof database inherited template objects: extensions=%d public_relations=%d", extensionCount, publicRelationCount)
	}
	var searchPath string
	if err := db.QueryRowContext(ctx, "SHOW search_path").Scan(&searchPath); err != nil {
		t.Fatalf("read disposable database search_path: %v", err)
	}
	if searchPath != "public" {
		t.Fatalf("search_path = %q, want public so operator classes resolve to this database's own installs", searchPath)
	}
	// The reported symptom end to end: a trigram GIN index with an explicit
	// gin_trgm_ops operator class builds on the disposable database.
	InstallTrigramExtension(ctx, t, db)
	if _, err := db.ExecContext(ctx, "CREATE TABLE public.eshu_proof_trgm_probe (value text)"); err != nil {
		t.Fatalf("create trigram probe table: %v", err)
	}
	if _, err := db.ExecContext(ctx, "CREATE INDEX ON public.eshu_proof_trgm_probe USING gin (value gin_trgm_ops)"); err != nil {
		t.Fatalf("create gin_trgm_ops index on the disposable database: %v", err)
	}
}

// disposableProofTemplate1DSN rewrites an administrative postgres-database DSN
// to the same cluster's template1, which is the database a template plant
// must connect to.
func disposableProofTemplate1DSN(t *testing.T, adminDSN string) string {
	t.Helper()
	parsed, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatalf("parse administrative DSN: %v", err)
	}
	parsed.Path = "/template1"
	parsed.RawPath = ""
	return parsed.String()
}

// assertProofDatabaseDropped fails when the named proof database still
// exists: the helper's cleanup must have dropped it, on the success path as
// well as on a failed open or ping, because the single drop is registered
// before the target opens instead of living on one branch.
func assertProofDatabaseDropped(t *testing.T, adminDSN, proofDBName string) {
	t.Helper()
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open administrative database for the survivor check: %v", err)
	}
	defer func() { _ = admin.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var survivors int
	if err := admin.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_database WHERE datname = $1`, proofDBName).Scan(&survivors); err != nil {
		t.Fatalf("list surviving proof databases: %v", err)
	}
	if survivors != 0 {
		t.Fatalf("proof database %q survived the helper cleanup", proofDBName)
	}
}
