// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

func TestPackageManifestConsumptionBackfillRepairsOldWriterAfterReadyLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedPackageManifestConsumptionBackfillScope(t, ctx, database)
	seedPackageManifestConsumptionBackfillFact(t, ctx, database, "manifest-before-ready", "lodash")

	if err := BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("initial BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	assertPackageManifestConsumptionReady(t, ctx, database, true)
	assertPackageManifestConsumptionKeyCount(t, ctx, database, 1)

	// This direct write deliberately has no sidecar-writer GUC. It simulates an
	// old ingester and must fence readers until the elected repair pass rebuilds
	// the scope under its ingestion_scopes lock.
	seedPackageManifestConsumptionBackfillFact(t, ctx, database, "manifest-after-ready", "left-pad")
	assertPackageManifestConsumptionReady(t, ctx, database, false)
	if err := BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("dirty BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	assertPackageManifestConsumptionReady(t, ctx, database, true)
	assertPackageManifestConsumptionKeyCount(t, ctx, database, 2)
}

func TestPackageManifestConsumptionBackfillPagesHeavyScopeLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedPackageManifestConsumptionBackfillScope(t, ctx, database)
	const factCount = packageManifestConsumptionKeyBackfillBatchSize * 3
	for index := 0; index < factCount; index++ {
		seedPackageManifestConsumptionBackfillFact(t, ctx, database,
			fmt.Sprintf("manifest-heavy-%04d", index), fmt.Sprintf("package-%04d", index))
	}

	started := time.Now()
	if err := BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	elapsed := time.Since(started)
	assertPackageManifestConsumptionReady(t, ctx, database, true)
	assertPackageManifestConsumptionKeyCount(t, ctx, database, factCount)
	log.Printf("event_name=package_manifest_consumption_keys.backfill.measurement facts=%d pages=%d scope_rebuild_elapsed_ms=%d", factCount, factCount/packageManifestConsumptionKeyBackfillBatchSize, elapsed.Milliseconds())
}

func seedPackageManifestConsumptionBackfillScope(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind,
     partition_key, observed_at, ingested_at, status, payload)
VALUES ('sidecar-live-scope', 'repository', 'git', 'repository:sidecar-live', 'git',
        'repository:sidecar-live', clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)`); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations
    (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('sidecar-live-scope', 'sidecar-live-generation', 'sync',
        clock_timestamp(), clock_timestamp(), 'active')`); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
}

type packageManifestConsumptionFactWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func seedPackageManifestConsumptionBackfillFact(t *testing.T, ctx context.Context, database packageManifestConsumptionFactWriter, factID, name string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, 'sidecar-live-scope', 'sidecar-live-generation', 'content_entity', $1,
        'git', $1, clock_timestamp(), clock_timestamp(), FALSE,
        jsonb_build_object(
            'repo_id', 'repository:sidecar-live',
            'entity_type', 'Variable',
            'entity_name', $2::text,
            'entity_metadata', jsonb_build_object(
                'config_kind', 'dependency', 'package_manager', 'npm')))
`, factID, name); err != nil {
		t.Fatalf("seed manifest fact %s: %v", factID, err)
	}
}

func assertPackageManifestConsumptionReady(t *testing.T, ctx context.Context, database *sql.DB, want bool) {
	t.Helper()
	ready, err := PackageManifestConsumptionKeysReady(ctx, SQLDB{DB: database})
	if err != nil {
		t.Fatalf("PackageManifestConsumptionKeysReady(): %v", err)
	}
	if ready != want {
		t.Fatalf("PackageManifestConsumptionKeysReady() = %t, want %t", ready, want)
	}
}

func assertPackageManifestConsumptionKeyCount(t *testing.T, ctx context.Context, database *sql.DB, want int) {
	t.Helper()
	var got int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM package_manifest_consumption_keys`).Scan(&got); err != nil {
		t.Fatalf("count sidecar keys: %v", err)
	}
	if got != want {
		t.Fatalf("sidecar key count = %d, want %d", got, want)
	}
}

func TestPackageManifestConsumptionBackfillWaitsForScopeWriterLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedPackageManifestConsumptionBackfillScope(t, ctx, database)
	seedPackageManifestConsumptionBackfillFact(t, ctx, database, "manifest-initial", "initial")
	if err := BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("initial BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	seedPackageManifestConsumptionBackfillFact(t, ctx, database, "manifest-dirty", "dirty")

	writer, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin writer transaction: %v", err)
	}
	defer func() { _ = writer.Rollback() }()
	if _, err := writer.ExecContext(ctx, `SELECT scope_id FROM ingestion_scopes WHERE scope_id = 'sidecar-live-scope' FOR UPDATE`); err != nil {
		t.Fatalf("lock writer scope: %v", err)
	}
	seedPackageManifestConsumptionBackfillFact(t, ctx, writer, "manifest-writer", "writer")

	done := make(chan error, 1)
	go func() { done <- BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}) }()
	select {
	case err := <-done:
		t.Fatalf("BackfillPackageManifestConsumptionKeys() returned before scope writer committed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("commit writer transaction: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("BackfillPackageManifestConsumptionKeys() after writer commit: %v", err)
	}
	assertPackageManifestConsumptionReady(t, ctx, database, true)
	assertPackageManifestConsumptionKeyCount(t, ctx, database, 3)
}

func TestPackageManifestConsumptionBackfillBoundsTwentyFiveScopePassLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	const scopes = packageManifestConsumptionKeyBackfillScopesPerPass
	const factsPerScope = 20
	for scopeIndex := 0; scopeIndex < scopes; scopeIndex++ {
		scopeID := fmt.Sprintf("sidecar-pass-%02d", scopeIndex)
		generationID := scopeID + "-generation"
		if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind,
     partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1,
        clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)`, scopeID); err != nil {
			t.Fatalf("seed scope %s: %v", scopeID, err)
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations
    (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'sync', clock_timestamp(), clock_timestamp(), 'active')`, scopeID, generationID); err != nil {
			t.Fatalf("seed generation %s: %v", scopeID, err)
		}
		for factIndex := 0; factIndex < factsPerScope; factIndex++ {
			factID := fmt.Sprintf("%s-fact-%02d", scopeID, factIndex)
			if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, 'content_entity', $1, 'git', $1,
        clock_timestamp(), clock_timestamp(), FALSE,
        jsonb_build_object('repo_id', $2::text, 'entity_type', 'Variable',
            'entity_name', $4::text,
            'entity_metadata', jsonb_build_object('config_kind', 'dependency', 'package_manager', 'npm')))
`, factID, scopeID, generationID, fmt.Sprintf("package-%d-%d", scopeIndex, factIndex)); err != nil {
				t.Fatalf("seed fact %s: %v", factID, err)
			}
		}
	}

	started := time.Now()
	if err := BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	elapsed := time.Since(started)
	assertPackageManifestConsumptionReady(t, ctx, database, false)
	assertPackageManifestConsumptionKeyCount(t, ctx, database, scopes*factsPerScope)
	log.Printf("event_name=package_manifest_consumption_keys.backfill.measurement scopes=%d facts=%d pass_elapsed_ms=%d", scopes, scopes*factsPerScope, elapsed.Milliseconds())
}

func TestPackageManifestConsumptionBackfillConcurrentPassesAreIdempotentLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	seedPackageManifestConsumptionBackfillScope(t, ctx, database)
	seedPackageManifestConsumptionBackfillFact(t, ctx, database, "manifest-initial", "initial")
	if err := BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("initial BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	seedPackageManifestConsumptionBackfillFact(t, ctx, database, "manifest-dirty", "dirty")

	results := make(chan error, 2)
	for range 2 {
		go func() { results <- BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}) }()
	}
	for pass := 0; pass < 2; pass++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent backfill pass %d: %v", pass, err)
		}
	}
	assertPackageManifestConsumptionReady(t, ctx, database, true)
	assertPackageManifestConsumptionKeyCount(t, ctx, database, 2)
}
