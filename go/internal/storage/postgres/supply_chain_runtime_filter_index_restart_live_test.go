// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

var runtimeFallbackIndexNames = []string{
	"fact_records_workload_identity_workload_trim_idx",
	"fact_records_workload_identity_entity_trim_idx",
	"fact_records_service_catalog_workload_trim_idx",
	"fact_records_service_catalog_service_trim_idx",
	"fact_records_ci_cd_environment_trim_idx",
}

// TestSupplyChainRuntimeFallbackIndexRestartSafetyLive proves the five new
// partial indexes against populated fact storage and active ordinary writers.
func TestSupplyChainRuntimeFallbackIndexRestartSafetyLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(t,
		os.Getenv("ESHU_TEST_SUPPLY_CHAIN_RUNTIME_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_SUPPLY_CHAIN_RUNTIME_INDEX_POSTGRES_DISPOSABLE"),
		4*time.Minute)
	before, indexes := runtimeFallbackIndexDefinitions(t)
	exec := SQLDB{DB: db}
	if err := applyBootstrapDefinitionsWith(ctx, exec, before, slog.Default(), schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("apply pre-upgrade definitions: %v", err)
	}
	seedRuntimeFallbackIndexRows(t, ctx, db)
	oldWorkload := readDocumentationIndexState(t, ctx, db, "fact_records_workload_identity_workload_idx")
	oldEntity := readDocumentationIndexState(t, ctx, db, "fact_records_workload_identity_entity_keys_idx")

	writerCtx, stopWriter := context.WithCancel(ctx)
	writerReady := make(chan error, 1)
	writerDone := make(chan runtimeFallbackWriterProgress, 1)
	go func() { writerDone <- runRuntimeFallbackWriter(writerCtx, db, writerReady) }()
	if err := <-writerReady; err != nil {
		stopWriter()
		<-writerDone
		t.Fatalf("start ordinary fact writer: %v", err)
	}
	started := time.Now()
	migrationErr := applyBootstrapDefinitionsWith(ctx, exec, indexes, slog.Default(), schemaBootstrapCoordination{})
	buildDuration := time.Since(started)
	stopWriter()
	writer := <-writerDone
	if migrationErr != nil {
		t.Fatalf("apply populated concurrent index migrations: %v", migrationErr)
	}
	if writer.err != nil {
		t.Fatalf("ordinary writer during index build: %v", writer.err)
	}
	if writer.cycles < 3 {
		t.Fatalf("ordinary writer made %d insert/update/delete cycles during index build, want at least 3", writer.cycles)
	}
	t.Logf("five populated concurrent indexes built in %s; writer cycles=%d, max cycle=%s", buildDuration, writer.cycles, writer.maxCycle)

	first := runtimeFallbackIndexStates(t, ctx, db)
	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("first full bootstrap after upgrade: %v", err)
	}
	if got := runtimeFallbackIndexStates(t, ctx, db); !equalRuntimeFallbackIndexStates(first, got) {
		t.Fatalf("first restart changed index states: before=%+v after=%+v", first, got)
	}
	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("second full bootstrap after upgrade: %v", err)
	}
	if got := runtimeFallbackIndexStates(t, ctx, db); !equalRuntimeFallbackIndexStates(first, got) {
		t.Fatalf("second restart changed index states: before=%+v after=%+v", first, got)
	}
	if got := readDocumentationIndexState(t, ctx, db, "fact_records_workload_identity_workload_idx"); got != oldWorkload {
		t.Fatalf("legacy scalar index changed during additive upgrade: before=%+v after=%+v", oldWorkload, got)
	}
	if got := readDocumentationIndexState(t, ctx, db, "fact_records_workload_identity_entity_keys_idx"); got != oldEntity {
		t.Fatalf("legacy entity index changed during additive upgrade: before=%+v after=%+v", oldEntity, got)
	}

	proveRuntimeFallbackInvalidIndexRecovery(t, ctx, db, first)
	proveRuntimeFallbackDropAndReapply(t, ctx, db, indexes[1])
}

func runtimeFallbackIndexDefinitions(t *testing.T) ([]Definition, []Definition) {
	t.Helper()
	defs := BootstrapDefinitions()
	first := -1
	for i, def := range defs {
		if def.Name == "supply_chain_identity_workload_trim_index" {
			first = i
			break
		}
	}
	if first < 0 || first+len(runtimeFallbackIndexNames) > len(defs) {
		t.Fatalf("five supply-chain runtime fallback index definitions are missing: first=%d, total=%d", first, len(defs))
	}
	for i, name := range runtimeFallbackIndexNames {
		if !strings.Contains(defs[first+i].SQL, name) {
			t.Fatalf("migration %s does not create %s", defs[first+i].Path, name)
		}
	}
	return defs[:first], defs[first : first+len(runtimeFallbackIndexNames)]
}

func seedRuntimeFallbackIndexRows(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status
) VALUES ('scope:runtime-index-proof', 'repository', 'proof', 'proof', 'proof',
  'proof', clock_timestamp(), clock_timestamp(), 'active');
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES ('generation:runtime-index-proof', 'scope:runtime-index-proof', 'proof',
  clock_timestamp(), clock_timestamp(), 'active', clock_timestamp());
UPDATE ingestion_scopes SET active_generation_id = 'generation:runtime-index-proof'
WHERE scope_id = 'scope:runtime-index-proof';
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT 'runtime-index:' || n, 'scope:runtime-index-proof',
  'generation:runtime-index-proof',
  CASE n % 3 WHEN 0 THEN 'reducer_workload_identity'
             WHEN 1 THEN 'reducer_service_catalog_correlation'
             ELSE 'reducer_ci_cd_run_correlation' END,
  'runtime-index:' || n, 'proof', 'runtime-index:' || n,
  clock_timestamp(), clock_timestamp(),
  jsonb_build_object(
    'workload_id', '  workload:runtime-index:' || n || '  ',
    'entity_keys', jsonb_build_array('  workload:runtime-index:' || n || '  '),
    'service_id', '  service:runtime-index:' || n || '  ',
    'environment', '  env:runtime-index:' || n || '  ',
    'outcome', 'exact')
FROM generate_series(1, 30000) AS n;
`)
	if err != nil {
		t.Fatalf("seed 30,000 populated runtime facts: %v", err)
	}
}

type runtimeFallbackWriterProgress struct {
	cycles   int
	maxCycle time.Duration
	err      error
}

func runRuntimeFallbackWriter(ctx context.Context, db *sql.DB, ready chan<- error) runtimeFallbackWriterProgress {
	var progress runtimeFallbackWriterProgress
	for n := 0; ; n++ {
		if ctx.Err() != nil {
			return progress
		}
		started := time.Now()
		if err := writeRuntimeFallbackCycle(ctx, db, n); err != nil {
			if ctx.Err() == nil {
				progress.err = err
			}
			if n == 0 {
				ready <- progress.err
			}
			return progress
		}
		progress.cycles++
		if elapsed := time.Since(started); elapsed > progress.maxCycle {
			progress.maxCycle = elapsed
		}
		if n == 0 {
			ready <- nil
		}
	}
}

func writeRuntimeFallbackCycle(ctx context.Context, db *sql.DB, n int) error {
	kind := []string{"reducer_workload_identity", "reducer_service_catalog_correlation", "reducer_ci_cd_run_correlation"}[n%3]
	id := fmt.Sprintf("runtime-writer:%d", n)
	padded := fmt.Sprintf("  workload:runtime-writer:%d  ", n)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, payload
) VALUES ($1, 'scope:runtime-index-proof', 'generation:runtime-index-proof',
  $2, $1, 'proof', $1, clock_timestamp(), clock_timestamp(),
  jsonb_build_object('workload_id', $3::text, 'entity_keys', jsonb_build_array($3::text),
    'service_id', $3::text, 'environment', $3::text, 'outcome', 'exact'))
ON CONFLICT (fact_id) DO UPDATE SET payload = EXCLUDED.payload, is_tombstone = FALSE`, id, kind, padded); err != nil {
		return fmt.Errorf("insert %s: %w", kind, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_records SET
  payload = jsonb_build_object('workload_id', $2::text, 'entity_keys', jsonb_build_array($2::text),
    'service_id', $2::text, 'environment', $2::text, 'outcome', 'exact'),
  is_tombstone = FALSE WHERE fact_id = $1`, id, strings.TrimSpace(padded)); err != nil {
		return fmt.Errorf("change normalized predicates %s: %w", kind, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_records SET
  payload = jsonb_build_object('workload_id', $2::text, 'entity_keys', jsonb_build_array($2::text),
    'service_id', $2::text, 'environment', $2::text, 'outcome', 'exact'),
  is_tombstone = TRUE WHERE fact_id = $1`, id, padded); err != nil {
		return fmt.Errorf("tombstone %s: %w", kind, err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM fact_records WHERE fact_id = $1`, id); err != nil {
		return fmt.Errorf("delete %s: %w", kind, err)
	}
	return nil
}

func runtimeFallbackIndexStates(t *testing.T, ctx context.Context, db *sql.DB) []documentationIndexState {
	t.Helper()
	states := make([]documentationIndexState, 0, len(runtimeFallbackIndexNames))
	for _, name := range runtimeFallbackIndexNames {
		state := readDocumentationIndexState(t, ctx, db, name)
		assertIndexReady(t, name, state)
		var size int64
		if err := db.QueryRowContext(ctx, "SELECT pg_relation_size($1::regclass)", name).Scan(&size); err != nil {
			t.Fatalf("size %s: %v", name, err)
		}
		t.Logf("index %s oid=%d bytes=%d definition=%s", name, state.oid, size, state.definition)
		states = append(states, state)
	}
	return states
}

func equalRuntimeFallbackIndexStates(a, b []documentationIndexState) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func proveRuntimeFallbackInvalidIndexRecovery(t *testing.T, ctx context.Context, db *sql.DB, first []documentationIndexState) {
	t.Helper()
	name := runtimeFallbackIndexNames[0]
	if _, err := db.ExecContext(ctx, "DROP INDEX CONCURRENTLY "+name); err != nil {
		t.Fatalf("drop valid index before interrupted-build proof: %v", err)
	}
	invalidDDL := "CREATE UNIQUE INDEX CONCURRENTLY " + name + " ON fact_records (fact_kind) WHERE fact_kind = 'reducer_workload_identity' AND is_tombstone = FALSE"
	if _, err := db.ExecContext(ctx, invalidDDL); err == nil {
		t.Fatal("duplicate unique build unexpectedly succeeded")
	}
	invalid := readDocumentationIndexState(t, ctx, db, name)
	if invalid.valid {
		t.Fatalf("failed concurrent build index %s is valid", name)
	}
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("recover invalid index through actual bootstrap: %v", err)
	}
	recovered := runtimeFallbackIndexStates(t, ctx, db)
	if recovered[0].oid == invalid.oid || recovered[0].definition != first[0].definition {
		t.Fatalf("recovered index differs: invalid=%+v first=%+v recovered=%+v", invalid, first[0], recovered[0])
	}
	for i := 1; i < len(first); i++ {
		if recovered[i] != first[i] {
			t.Fatalf("recovering %s changed unrelated index %s", name, runtimeFallbackIndexNames[i])
		}
	}
}

func proveRuntimeFallbackDropAndReapply(t *testing.T, ctx context.Context, db *sql.DB, def Definition) {
	t.Helper()
	name := runtimeFallbackIndexNames[1]
	first := readDocumentationIndexState(t, ctx, db, name)
	if _, err := db.ExecContext(ctx, "DROP INDEX CONCURRENTLY "+name); err != nil {
		t.Fatalf("drop %s to prove disposable schema rollback: %v", name, err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM eshu_schema_migrations WHERE path = $1 AND variant = 'full'", def.Path); err != nil {
		t.Fatalf("clear disposable migration ledger for %s: %v", name, err)
	}
	if err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: db}, []Definition{def}, slog.Default(), schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("reapply %s through actual migrator: %v", name, err)
	}
	got := readDocumentationIndexState(t, ctx, db, name)
	assertIndexReady(t, name, got)
	if got.oid == first.oid || got.definition != first.definition {
		t.Fatalf("reapplied %s differs: before=%+v after=%+v", name, first, got)
	}
}
