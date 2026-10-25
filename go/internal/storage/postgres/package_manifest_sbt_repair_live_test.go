// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8961 disposable PostgreSQL 18 proof that the reducer's sbt key repair adds a stored keyless sbt row's registry keys without a dirty row or a readiness flip, leaves a non-sbt scope's sidecar rows alone, converges in a second zero-insert walk that writes its own marker, runs only once the v2 marker exists, and is a no-op on databases with no sbt rows; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/consumption"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

var sbtRepairWantKeys = []string{
	"sbt-binary|maven|org.typelevel/cats-core_2.13",
	"sbt-binary|maven|org.typelevel:cats-core_2.13",
}

func openSBTRepairDatabase(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN")
	optIn := os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE")
	ctx, database := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 3*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	return ctx, database
}

func markSBTRepairV2Ready(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	if _, err := database.ExecContext(ctx, markPackageManifestConsumptionKeyBackfillSQL,
		PackageManifestConsumptionKeyBackfillMarker, time.Now().UTC()); err != nil {
		t.Fatalf("mark v2 backfill ready: %v", err)
	}
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

func sbtRepairPayload(scope, name string, metadata map[string]any) map[string]any {
	metadata["config_kind"] = "dependency"
	return map[string]any{
		"repo_id": "repository:" + scope, "entity_type": "Variable",
		"entity_name": name, "entity_metadata": metadata,
	}
}

// seedSBTRepairFact writes one dependency fact the way the ingester does, with
// the sidecar-writer setting on so the dirty trigger stays quiet. keyed=false
// leaves the sidecar empty, the state the code before #8961 left an sbt row in.
func seedSBTRepairFact(t *testing.T, ctx context.Context, database *sql.DB, scope, factID, name string, metadata map[string]any, keyed bool) {
	t.Helper()
	payload := sbtRepairPayload(scope, name, metadata)
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

func sbtRepairKeys(t *testing.T, ctx context.Context, database *sql.DB, scope string) []string {
	t.Helper()
	return sbtRepairStrings(t, ctx, database,
		`SELECT fact_id || '|' || ecosystem || '|' || package_name FROM package_manifest_consumption_keys WHERE scope_id = $1 ORDER BY 1`, scope)
}

func sbtRepairMarkerPresent(t *testing.T, ctx context.Context, database *sql.DB) bool {
	t.Helper()
	return len(sbtRepairStrings(t, ctx, database,
		`SELECT marker_name FROM package_manifest_consumption_key_backfill_markers WHERE marker_name = $1`, consumption.SBTRepairMarker)) == 1
}

func seedStoredSBTAndNPMScopes(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
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
}

// TestPackageManifestSBTRepairOldLogicRunLeavesRepairStateUntouched runs only
// the v2 backfill, which is all a binary older than the repair runs. Stored sbt
// rows stay keyless and no repair marker or progress row appears, so nothing an
// old reducer does can consume or hide the repair (#8961).
func TestPackageManifestSBTRepairOldLogicRunLeavesRepairStateUntouched(t *testing.T) {
	ctx, database := openSBTRepairDatabase(t)
	seedStoredSBTAndNPMScopes(t, ctx, database)
	markSBTRepairV2Ready(t, ctx, database)

	if err := (PackageManifestConsumptionKeyBackfill{}).runKeyBackfill(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("runKeyBackfill(): %v", err)
	}
	if keys := sbtRepairKeys(t, ctx, database, "scala-repo"); len(keys) != 0 {
		t.Fatalf("scala-repo keys = %v, want none from the v2 flow alone", keys)
	}
	if sbtRepairMarkerPresent(t, ctx, database) {
		t.Fatal("sbt repair marker exists after a v2-only run")
	}
	if progress := sbtRepairStrings(t, ctx, database,
		`SELECT marker_name FROM package_manifest_consumption_key_backfill_progress WHERE marker_name LIKE 'package_manifest_consumption_keys_sbt_repair%'`); len(progress) != 0 {
		t.Fatalf("repair progress rows after a v2-only run: %v", progress)
	}
}

// TestPackageManifestSBTRepairAddsKeysWithoutFlippingReadiness proves the
// repair end to end while a second connection polls readiness. The dirty set
// stays empty and readiness stays true throughout; the first walk inserts and
// restarts, the second inserts nothing and writes the marker; a non-sbt
// scope's sidecar rows are not rewritten.
func TestPackageManifestSBTRepairAddsKeysWithoutFlippingReadiness(t *testing.T) {
	ctx, database := openSBTRepairDatabase(t)
	seedStoredSBTAndNPMScopes(t, ctx, database)
	markSBTRepairV2Ready(t, ctx, database)
	nodeBefore := sbtRepairStrings(t, ctx, database,
		`SELECT fact_id || '|' || package_name || '|' || xmin::text FROM package_manifest_consumption_keys WHERE scope_id = 'node-repo' ORDER BY 1`)
	if len(nodeBefore) == 0 {
		t.Fatal("seed produced no node-repo keys")
	}

	probeCtx, stopProbe := context.WithCancel(ctx)
	var probes, notReady int
	var probeErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for probeCtx.Err() == nil {
			ready, err := PackageManifestConsumptionKeysReady(probeCtx, SQLDB{DB: database})
			if err != nil {
				if probeCtx.Err() == nil {
					probeErr = err
				}
				return
			}
			probes++
			if !ready {
				notReady++
			}
		}
	}()

	var reports []consumption.SBTRepairReport
	for pass := 0; pass < 4 && (len(reports) == 0 || reports[len(reports)-1].Pending); pass++ {
		report, err := (PackageManifestConsumptionKeyBackfill{}).RunWithReport(ctx, SQLDB{DB: database})
		if err != nil {
			t.Fatalf("RunWithReport() pass %d: %v", pass, err)
		}
		reports = append(reports, report)
	}
	stopProbe()
	wg.Wait()

	if probeErr != nil {
		t.Fatalf("readiness probe: %v", probeErr)
	}
	if probes == 0 || notReady != 0 {
		t.Fatalf("readiness probes = %d, not ready = %d, want probes > 0 and none not ready", probes, notReady)
	}
	if len(reports) != 2 {
		t.Fatalf("passes = %d (%+v), want two: an inserting walk, then a zero-insert walk", len(reports), reports)
	}
	first, second := reports[0], reports[1]
	if !first.Pending || !first.Restarted || first.InsertedRows != 2 {
		t.Fatalf("first walk = %+v, want pending, restarted, 2 inserted rows", first)
	}
	if second.Pending || !second.Completed || second.InsertedRows != 0 || second.Cursor != "scala-repo" {
		t.Fatalf("second walk = %+v, want complete with 0 inserted rows and cursor scala-repo", second)
	}
	if dirty := sbtRepairStrings(t, ctx, database, `SELECT scope_id FROM package_manifest_consumption_key_dirty_scopes`); len(dirty) != 0 {
		t.Fatalf("dirty scopes = %v, want none: the repair never writes the fence", dirty)
	}
	if got := sbtRepairKeys(t, ctx, database, "scala-repo"); !reflect.DeepEqual(got, sbtRepairWantKeys) {
		t.Fatalf("scala-repo keys = %v, want %v (artifact only, none for the row without one)", got, sbtRepairWantKeys)
	}
	if nodeAfter := sbtRepairStrings(t, ctx, database,
		`SELECT fact_id || '|' || package_name || '|' || xmin::text FROM package_manifest_consumption_keys WHERE scope_id = 'node-repo' ORDER BY 1`); !reflect.DeepEqual(nodeAfter, nodeBefore) {
		t.Fatalf("node-repo sidecar rows changed: before %v, after %v", nodeBefore, nodeAfter)
	}
	if !sbtRepairMarkerPresent(t, ctx, database) {
		t.Fatal("sbt repair marker missing after the zero-insert walk")
	}
	again, err := (PackageManifestConsumptionKeyBackfill{}).RunWithReport(ctx, SQLDB{DB: database})
	if err != nil || again.Pending || again.Scopes != 0 || again.InsertedRows != 0 {
		t.Fatalf("pass after the marker = %+v, err %v, want a no-op", again, err)
	}
}

// TestPackageManifestSBTRepairIsANoOpWithoutSBTRows covers a database with no
// sbt rows, with and without a v2 marker: the first pass writes the repair
// marker having inserted nothing.
func TestPackageManifestSBTRepairIsANoOpWithoutSBTRows(t *testing.T) {
	ctx, database := openSBTRepairDatabase(t)
	seedSBTRepairScope(t, ctx, database, "node-repo")
	seedSBTRepairFact(t, ctx, database, "node-repo", "npm-left-pad", "left-pad", map[string]any{"package_manager": "npm"}, false)

	report, err := (PackageManifestConsumptionKeyBackfill{}).RunWithReport(ctx, SQLDB{DB: database})
	if err != nil {
		t.Fatalf("RunWithReport(): %v", err)
	}
	if report.Pending || !report.Completed || report.InsertedRows != 0 || report.Scopes != 0 {
		t.Fatalf("report = %+v, want completed on the first pass with nothing inserted", report)
	}
	if !sbtRepairMarkerPresent(t, ctx, database) {
		t.Fatal("repair marker missing")
	}
}

// TestPackageManifestSBTRepairWaitsForTheV2Marker proves the repair does not
// start while the initial backfill is still walking scopes.
func TestPackageManifestSBTRepairWaitsForTheV2Marker(t *testing.T) {
	ctx, database := openSBTRepairDatabase(t)
	for i := 0; i < packageManifestConsumptionKeyBackfillScopesPerPass+1; i++ {
		scope := fmt.Sprintf("a-scope-%03d", i)
		seedSBTRepairScope(t, ctx, database, scope)
		seedSBTRepairFact(t, ctx, database, scope, scope+"-fact", "left-pad", map[string]any{"package_manager": "npm"}, false)
	}
	seedSBTRepairScope(t, ctx, database, "z-scala")
	seedSBTRepairFact(t, ctx, database, "z-scala", "z-sbt", "o:n", map[string]any{
		"package_manager": "sbt", "sbt_cross_version": "binary", "sbt_binary_artifact": "o:n_3",
	}, false)

	report, err := (PackageManifestConsumptionKeyBackfill{}).RunWithReport(ctx, SQLDB{DB: database})
	if err != nil {
		t.Fatalf("RunWithReport(): %v", err)
	}
	if report.Scopes != 0 || report.Pending || sbtRepairMarkerPresent(t, ctx, database) {
		t.Fatalf("report = %+v before the v2 marker: the repair must not run", report)
	}
	if progress := sbtRepairStrings(t, ctx, database,
		`SELECT marker_name FROM package_manifest_consumption_key_backfill_progress WHERE marker_name LIKE 'package_manifest_consumption_keys_sbt_repair%'`); len(progress) != 0 {
		t.Fatalf("repair progress rows before the v2 marker: %v", progress)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(encoded)
}
