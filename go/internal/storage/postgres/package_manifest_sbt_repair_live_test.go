// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8961 disposable PostgreSQL 18 proof that the sbt repair migration marks only the scopes holding sbt dependency rows dirty, that the existing backfill repair then gives a stored keyless sbt row its registry keys while a non-sbt scope's sidecar rows are not rewritten, and that re-running or racing the statement is safe; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const sbtRepairMigrationSuffix = "_package_manifest_consumption_keys_sbt_repair.sql"

func sbtRepairMigrationSQL(t *testing.T) string {
	t.Helper()
	for _, def := range migrations.BootstrapDefinitions() {
		if strings.HasSuffix(def.Path, sbtRepairMigrationSuffix) {
			return def.SQL
		}
	}
	t.Fatalf("no bootstrap definition ends with %s", sbtRepairMigrationSuffix)
	return ""
}

func seedSBTRepairScope(t *testing.T, ctx context.Context, database *sql.DB, scope string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind,
     partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, clock_timestamp(), clock_timestamp(), 'active', $1 || '-gen', '{}'::jsonb)`, scope); err != nil {
		t.Fatalf("seed scope %s: %v", scope, err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $1 || '-gen', 'sync', clock_timestamp(), clock_timestamp(), 'active')`, scope); err != nil {
		t.Fatalf("seed generation %s: %v", scope, err)
	}
}

// seedSBTRepairFact writes one dependency fact the way the ingester does, with
// the sidecar-writer setting on so the dirty trigger stays quiet, and writes
// its sidecar keys with the current code. An sbt row stored by the code before
// this change is simulated by passing keyed=false.
func seedSBTRepairFact(t *testing.T, ctx context.Context, database *sql.DB, scope, factID, name string, metadata map[string]any, keyed bool) {
	t.Helper()
	metadata["config_kind"] = "dependency"
	payload := map[string]any{
		"repo_id": "repository:" + scope, "entity_type": "Variable",
		"entity_name": name, "entity_metadata": metadata,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s: %v", factID, err)
	}
	tx, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin %s: %v", factID, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, setPackageManifestConsumptionKeysWriterSQL); err != nil {
		t.Fatalf("set writer %s: %v", factID, err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $2 || '-gen', 'content_entity', $1, 'git', $1, clock_timestamp(), clock_timestamp(), FALSE, $3::jsonb)`,
		factID, scope, string(encoded)); err != nil {
		t.Fatalf("insert %s: %v", factID, err)
	}
	if keyed {
		envelope := facts.Envelope{
			FactID: factID, ScopeID: scope, GenerationID: scope + "-gen", FactKind: "content_entity",
			Payload: payload, SourceRef: facts.Ref{SourceSystem: "git"},
		}
		if err := refreshPackageManifestConsumptionKeys(ctx, tx, []facts.Envelope{envelope}); err != nil {
			t.Fatalf("refresh keys %s: %v", factID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit %s: %v", factID, err)
	}
}

func sbtRepairStrings(t *testing.T, ctx context.Context, database *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// TestPackageManifestSBTRepairMigrationGivesStoredSBTRowsKeysLive proves the
// repair end to end. Scope scala-repo holds a stored sbt %% row and a %% row
// whose artifact is unknown, both keyless as the code before this change left
// them; scope node-repo holds a keyed npm row. The migration statement must
// mark only scala-repo dirty, the shipped backfill must then give the %% row
// its Maven artifact key, and node-repo's sidecar rows must not be rewritten.
func TestPackageManifestSBTRepairMigrationGivesStoredSBTRowsKeysLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 3*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	for _, scope := range []string{"scala-repo", "node-repo"} {
		seedSBTRepairScope(t, ctx, database, scope)
	}
	seedSBTRepairFact(t, ctx, database, "scala-repo", "sbt-binary", "org.typelevel:cats-core", map[string]any{
		"package_manager": "sbt", "sbt_cross_version": "binary", "sbt_binary_artifact": "org.typelevel:cats-core_2.13",
	}, false)
	seedSBTRepairFact(t, ctx, database, "scala-repo", "sbt-noartifact", "org.other:lib", map[string]any{
		"package_manager": "sbt", "sbt_cross_version": "binary",
	}, false)
	seedSBTRepairFact(t, ctx, database, "node-repo", "npm-left-pad", "left-pad", map[string]any{"package_manager": "npm"}, true)
	if _, err := database.ExecContext(ctx, markPackageManifestConsumptionKeyBackfillSQL,
		PackageManifestConsumptionKeyBackfillMarker, time.Now().UTC()); err != nil {
		t.Fatalf("mark backfill ready: %v", err)
	}
	ready, err := PackageManifestConsumptionKeysReady(ctx, SQLDB{DB: database})
	if err != nil || !ready {
		t.Fatalf("before the repair ready = %v, err = %v, want true", ready, err)
	}
	nodeKeysBefore := sbtRepairStrings(t, ctx, database,
		`SELECT fact_id || '|' || package_name || '|' || xmin::text FROM package_manifest_consumption_keys WHERE scope_id = 'node-repo' ORDER BY 1`)
	if len(nodeKeysBefore) == 0 {
		t.Fatal("seed produced no node-repo keys")
	}
	if keys := sbtRepairStrings(t, ctx, database,
		`SELECT fact_id FROM package_manifest_consumption_keys WHERE scope_id = 'scala-repo'`); len(keys) != 0 {
		t.Fatalf("stored sbt rows already have keys %v, want the keyless pre-change state", keys)
	}

	repair := sbtRepairMigrationSQL(t)
	if _, err := database.ExecContext(ctx, repair); err != nil {
		t.Fatalf("run repair statement: %v", err)
	}
	if dirty := sbtRepairStrings(t, ctx, database,
		`SELECT scope_id FROM package_manifest_consumption_key_dirty_scopes ORDER BY 1`); !reflect.DeepEqual(dirty, []string{"scala-repo"}) {
		t.Fatalf("dirty scopes = %v, want only [scala-repo]", dirty)
	}
	if ready, _ := PackageManifestConsumptionKeysReady(ctx, SQLDB{DB: database}); ready {
		t.Fatal("ready = true with a dirty scope, want readers to wait for the repair")
	}

	if err := BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("BackfillPackageManifestConsumptionKeys(): %v", err)
	}
	if ready, err := PackageManifestConsumptionKeysReady(ctx, SQLDB{DB: database}); err != nil || !ready {
		t.Fatalf("after the repair ready = %v, err = %v, want true", ready, err)
	}
	scalaKeys := sbtRepairStrings(t, ctx, database,
		`SELECT fact_id || '|' || ecosystem || '|' || package_name FROM package_manifest_consumption_keys WHERE scope_id = 'scala-repo' ORDER BY 1`)
	wantScala := []string{
		"sbt-binary|maven|org.typelevel/cats-core_2.13",
		"sbt-binary|maven|org.typelevel:cats-core_2.13",
	}
	if !reflect.DeepEqual(scalaKeys, wantScala) {
		t.Fatalf("scala-repo keys = %v, want %v (artifact only, none for the row without one)", scalaKeys, wantScala)
	}
	if nodeKeysAfter := sbtRepairStrings(t, ctx, database,
		`SELECT fact_id || '|' || package_name || '|' || xmin::text FROM package_manifest_consumption_keys WHERE scope_id = 'node-repo' ORDER BY 1`); !reflect.DeepEqual(nodeKeysAfter, nodeKeysBefore) {
		t.Fatalf("node-repo sidecar rows changed: before %v, after %v", nodeKeysBefore, nodeKeysAfter)
	}

	// Re-running the statement re-marks the scope and the repair converges to the
	// same keys: the migration is safe to apply again.
	if _, err := database.ExecContext(ctx, repair); err != nil {
		t.Fatalf("re-run repair statement: %v", err)
	}
	if err := BackfillPackageManifestConsumptionKeys(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if again := sbtRepairStrings(t, ctx, database,
		`SELECT fact_id || '|' || ecosystem || '|' || package_name FROM package_manifest_consumption_keys WHERE scope_id = 'scala-repo' ORDER BY 1`); !reflect.DeepEqual(again, wantScala) {
		t.Fatalf("keys after a second repair = %v, want %v", again, wantScala)
	}
}

// TestPackageManifestSBTRepairStatementIsSafeToRaceLive runs the statement from
// several sessions at once while a dirty row for the same scope already exists,
// and requires no error, one row per scope, and a new row version on every
// re-mark: a repair that is rewriting the scope compares xmin and so keeps the
// row instead of clearing a mark made after it started.
func TestPackageManifestSBTRepairStatementIsSafeToRaceLive(t *testing.T) {
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 3*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	scopes := []string{"scala-a", "scala-b", "scala-c"}
	for _, scope := range scopes {
		seedSBTRepairScope(t, ctx, database, scope)
		seedSBTRepairFact(t, ctx, database, scope, scope+"-fact", "o:n", map[string]any{
			"package_manager": "sbt", "sbt_cross_version": "binary", "sbt_binary_artifact": "o:n_3",
		}, false)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO package_manifest_consumption_key_dirty_scopes (scope_id) VALUES ('scala-b')`); err != nil {
		t.Fatalf("pre-existing dirty row: %v", err)
	}
	xminBefore := sbtRepairStrings(t, ctx, database,
		`SELECT xmin::text FROM package_manifest_consumption_key_dirty_scopes WHERE scope_id = 'scala-b'`)

	repair := sbtRepairMigrationSQL(t)
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := database.ExecContext(ctx, repair)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent repair statement: %v", err)
		}
	}
	if dirty := sbtRepairStrings(t, ctx, database,
		`SELECT scope_id FROM package_manifest_consumption_key_dirty_scopes ORDER BY 1`); !reflect.DeepEqual(dirty, scopes) {
		t.Fatalf("dirty scopes = %v, want %v once each", dirty, scopes)
	}
	xminAfter := sbtRepairStrings(t, ctx, database,
		`SELECT xmin::text FROM package_manifest_consumption_key_dirty_scopes WHERE scope_id = 'scala-b'`)
	if reflect.DeepEqual(xminBefore, xminAfter) {
		t.Fatalf("scala-b dirty row kept xmin %v, want a new row version from the re-mark", xminBefore)
	}
}
