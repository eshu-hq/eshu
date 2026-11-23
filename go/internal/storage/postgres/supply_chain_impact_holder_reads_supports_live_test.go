// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The finding-fact reads #11197 retired, kept verbatim as the oracle the
// supports reads must equal.
const (
	oracleRepositoryHoldersSQL = `
SELECT DISTINCT finding.scope_id
FROM fact_records AS finding
JOIN ingestion_scopes AS scope
  ON scope.scope_id = finding.scope_id
 AND scope.active_generation_id = finding.generation_id
WHERE finding.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
  AND finding.is_tombstone = FALSE
  AND finding.payload->>'repository_id' = ANY($1::text[])
  AND finding.scope_id <> $2
ORDER BY finding.scope_id`
	oracleScopedPackagesSQL = `
SELECT DISTINCT payload->>'cve_id',
       COALESCE(payload->>'advisory_id', ''),
       COALESCE(payload->>'package_id', ''),
       COALESCE(payload->>'purl', ''),
       COALESCE(payload->>'package_name', ''),
       COALESCE(payload->>'ecosystem', '')
FROM fact_records
WHERE fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
  AND scope_id = $1 AND generation_id = $2 AND is_tombstone = FALSE
  AND payload->>'cve_id' = ANY($3::text[])`
	oracleOutOfStampSQL = `
SELECT EXISTS (SELECT 1 FROM fact_records
WHERE fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
  AND scope_id = $1 AND generation_id = $2 AND is_tombstone = FALSE
  AND payload->>'cve_id' <> '' AND NOT (payload->>'cve_id' = ANY($3::text[])))`
)

// holderReadsSeed writes the multi-support corpus as finding facts: one CVE held
// by several scopes, a tombstoned copy, a stale-generation copy, rows with a
// missing and an empty cve id, and a row with no repository id.
func holderReadsSeed(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	fanOutSeedScope(t, ctx, db, "scope-a", "gen-2", "gen-1")
	fanOutSeedScope(t, ctx, db, "scope-b", "gen-2")
	fanOutSeedScope(t, ctx, db, "scope-c", "gen-1")
	for _, f := range []struct {
		id, scope, gen, payload string
		tombstone               bool
	}{
		{"f1", "scope-a", "gen-2", `{"cve_id":"CVE-X","advisory_id":"GHSA-1","package_id":"p1","repository_id":"R1","purl":"pkg:npm/p1@1","package_name":"p1","ecosystem":"NPM"}`, false},
		{"f2", "scope-a", "gen-2", `{"cve_id":"CVE-X","advisory_id":"GHSA-1","package_id":"p1","repository_id":"R1","purl":"pkg:npm/p1@1","package_name":"p1","ecosystem":"NPM"}`, false},
		{"f3", "scope-a", "gen-2", `{"cve_id":"CVE-Y","package_id":"p2","repository_id":"R2"}`, false},
		{"f4", "scope-a", "gen-2", `{"cve_id":"CVE-X","package_id":"p-tomb","repository_id":"R1"}`, true},
		{"f5", "scope-a", "gen-1", `{"cve_id":"CVE-X","package_id":"p-stale","repository_id":"R1"}`, false},
		{"f6", "scope-b", "gen-2", `{"cve_id":"CVE-X","package_id":"p1","repository_id":"R1"}`, false},
		{"f7", "scope-b", "gen-2", `{"cve_id":"CVE-Z","package_id":"p3","repository_id":"R3","ecosystem":"pypi"}`, false},
		{"f8", "scope-c", "gen-1", `{"cve_id":"CVE-W","package_id":"p4","repository_id":"R1"}`, false},
		{"f9", "scope-c", "gen-1", `{"package_id":"p5","repository_id":"R1"}`, false},
		{"f10", "scope-c", "gen-1", `{"cve_id":"","package_id":"p6","repository_id":"R2"}`, false},
		{"f11", "scope-b", "gen-2", `{"cve_id":"CVE-X","package_id":"p7"}`, false},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'synthetic', $1, now(), now(), $5, $6::jsonb)`,
			f.id, f.scope, fanOutGen(f.scope, f.gen), facts.ReducerSupplyChainImpactFindingFactKind, f.tombstone, f.payload); err != nil {
			t.Fatalf("seed finding %s: %v", f.id, err)
		}
	}
}

// settleSupplyChainImpactSupports fills the supports table from the finding facts
// a test wrote directly (no maintainer ran) and drains the old-writer fence that
// each direct insert marked, so the readiness check of the holder, seed and
// fence reads passes (#11197). It is a no-op when nothing is missing.
func settleSupplyChainImpactSupports(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: db}, ChunkSize: 100}
	for step := 0; step < 500; step++ {
		report, err := backfill.RunStep(ctx)
		if err != nil {
			t.Fatalf("supports backfill step %d: %v", step, err)
		}
		if report.Complete && report.DirtyScopes == 0 {
			return
		}
	}
	t.Fatal("supports backfill did not settle in 500 steps")
}

func holderReadsOracleStrings(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("oracle read: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("oracle scan: %v", err)
		}
		out = append(out, s)
	}
	return out
}

// TestSupplyChainImpactSupportsHolderReadsEqualFactReadsLive is the #11197 RED/GREEN
// equality: every read returns exactly what the finding-fact read returned, and
// keeps returning it once the finding facts are gone, as they will be after the
// write stop.
func TestSupplyChainImpactSupportsHolderReadsEqualFactReadsLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	holderReadsSeed(t, ctx, db)
	store := postgres.SQLDB{DB: db}
	supportsBackfillRunToCompletion(t, ctx, postgres.SupplyChainImpactSupportsBackfill{DB: store, ChunkSize: 3})
	if ready, err := postgres.SupplyChainImpactSupportsReady(ctx, store); err != nil || !ready {
		t.Fatalf("supports ready = %v (err %v), want true after the backfill", ready, err)
	}

	type scopedCase struct {
		scope, gen string
		cves       []string
	}
	repoCases := []struct {
		own   string
		repos []string
	}{
		{"scope-a", []string{"R1"}},
		{"scope-b", []string{"R1"}},
		{"scope-c", []string{"R1", "R2"}},
		{"scope-a", []string{"R1", "R2", "R3"}},
		{"scope-a", []string{"R-none"}},
		{"scope-a", []string{""}},
		{"scope-a", []string{"R1", ""}},
	}
	scopedCases := []scopedCase{
		{"scope-a", "gen-2", []string{"CVE-X"}},
		{"scope-a", "gen-2", []string{"CVE-X", "CVE-Y"}},
		{"scope-a", "gen-1", []string{"CVE-X"}},
		{"scope-b", "gen-2", []string{"CVE-X", "CVE-Z", "CVE-none"}},
		{"scope-c", "gen-1", []string{"CVE-W", ""}},
		{"scope-c", "gen-1", []string{""}},
		{"scope-a", "gen-2", []string{"CVE-none"}},
	}
	outCases := []scopedCase{
		{"scope-a", "gen-2", []string{"CVE-X"}},
		{"scope-a", "gen-2", []string{"CVE-X", "CVE-Y"}},
		{"scope-a", "gen-2", []string{"CVE-Y"}},
		{"scope-c", "gen-1", []string{"CVE-W"}},
		{"scope-c", "gen-1", []string{"CVE-none"}},
		{"scope-b", "gen-2", []string{"CVE-X"}},
		{"scope-b", "gen-2", []string{"CVE-X", "CVE-Z"}},
	}

	wantRepo := make([][]string, len(repoCases))
	for i, c := range repoCases {
		wantRepo[i] = holderReadsOracleStrings(t, ctx, db, oracleRepositoryHoldersSQL, array.StringArray(c.repos), c.own)
	}
	wantScoped := make([][]supplychaincore.ScopedFindingPackage, len(scopedCases))
	for i, c := range scopedCases {
		rows, err := db.QueryContext(ctx, oracleScopedPackagesSQL, c.scope, fanOutGen(c.scope, c.gen), array.StringArray(c.cves))
		if err != nil {
			t.Fatalf("oracle scoped: %v", err)
		}
		for rows.Next() {
			var r supplychaincore.ScopedFindingPackage
			if err := rows.Scan(&r.CVEID, &r.AdvisoryID, &r.PackageID, &r.PURL, &r.PackageName, &r.Ecosystem); err != nil {
				t.Fatalf("oracle scoped scan: %v", err)
			}
			if r.CVEID != "" {
				wantScoped[i] = append(wantScoped[i], r)
			}
		}
		_ = rows.Close()
	}
	wantOut := make([]bool, len(outCases))
	for i, c := range outCases {
		if err := db.QueryRowContext(ctx, oracleOutOfStampSQL, c.scope, fanOutGen(c.scope, c.gen), array.StringArray(c.cves)).Scan(&wantOut[i]); err != nil {
			t.Fatalf("oracle out-of-stamp: %v", err)
		}
	}
	if len(wantRepo[0]) == 0 || len(wantScoped[0]) == 0 {
		t.Fatal("test bug: the corpus must make the first cases non-empty")
	}

	// The write stop: the finding facts are gone, only supports remain.
	if _, err := db.ExecContext(ctx, `DELETE FROM fact_records WHERE fact_kind = $1`, facts.ReducerSupplyChainImpactFindingFactKind); err != nil {
		t.Fatalf("delete finding facts: %v", err)
	}

	fanOut := fanOutLiveStore(db)
	for i, c := range repoCases {
		got, err := fanOut.FindRepositoryFindingHolders(ctx, c.own, c.repos)
		if err != nil {
			t.Fatalf("repository holders %+v: %v", c, err)
		}
		if !sameStrings(got, wantRepo[i]) {
			t.Errorf("repository holders %+v = %v, want the fact read's %v", c, got, wantRepo[i])
		}
	}
	facts := postgres.NewFactStore(store)
	for i, c := range scopedCases {
		got, err := facts.ListScopedSupplyChainImpactFindingPackages(ctx, c.scope, fanOutGen(c.scope, c.gen), c.cves)
		if err != nil {
			t.Fatalf("scoped packages %+v: %v", c, err)
		}
		if !sameScopedPackages(got, wantScoped[i]) {
			t.Errorf("scoped packages %+v = %+v, want the fact read's %+v", c, got, wantScoped[i])
		}
	}
	for i, c := range outCases {
		got, err := facts.HasOutOfStampActiveSupplyChainImpactRows(ctx, c.scope, fanOutGen(c.scope, c.gen), c.cves)
		if err != nil {
			t.Fatalf("out-of-stamp %+v: %v", c, err)
		}
		if got != wantOut[i] {
			t.Errorf("out-of-stamp %+v = %v, want the fact read's %v", c, got, wantOut[i])
		}
	}
}

func sameStrings(a, b []string) bool {
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return reflect.DeepEqual(x, y) || (len(x) == 0 && len(y) == 0)
}

func sameScopedPackages(a, b []supplychaincore.ScopedFindingPackage) bool {
	key := func(p supplychaincore.ScopedFindingPackage) string {
		return p.CVEID + "|" + p.AdvisoryID + "|" + p.PackageID + "|" + p.PURL + "|" + p.PackageName + "|" + p.Ecosystem
	}
	ka, kb := []string{}, []string{}
	for _, p := range a {
		ka = append(ka, key(p))
	}
	for _, p := range b {
		kb = append(kb, key(p))
	}
	return sameStrings(ka, kb)
}

// TestSupplyChainImpactSupportsHolderReadsRefuseNotReadyLive proves the
// reads never serve a partial table: with no backfill marker, or with the
// old-writer fence holding a dirty scope, each returns ErrSupportsNotReady.
func TestSupplyChainImpactSupportsHolderReadsRefuseNotReadyLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	holderReadsSeed(t, ctx, db)
	store := postgres.SQLDB{DB: db}
	assertRefused := func(label string) {
		t.Helper()
		fanOut := fanOutLiveStore(db)
		if _, err := fanOut.FindRepositoryFindingHolders(ctx, "scope-a", []string{"R1"}); !errors.Is(err, supplychaincore.ErrSupportsNotReady) {
			t.Errorf("%s: repository holders error = %v, want ErrSupportsNotReady", label, err)
		}
		facts := postgres.NewFactStore(store)
		if _, err := facts.ListScopedSupplyChainImpactFindingPackages(ctx, "scope-a", fanOutGen("scope-a", "gen-2"), []string{"CVE-X"}); !errors.Is(err, supplychaincore.ErrSupportsNotReady) {
			t.Errorf("%s: scoped packages error = %v, want ErrSupportsNotReady", label, err)
		}
		if _, err := facts.HasOutOfStampActiveSupplyChainImpactRows(ctx, "scope-a", fanOutGen("scope-a", "gen-2"), []string{"CVE-X"}); !errors.Is(err, supplychaincore.ErrSupportsNotReady) {
			t.Errorf("%s: out-of-stamp error = %v, want ErrSupportsNotReady", label, err)
		}
	}
	assertRefused("no backfill marker")

	supportsBackfillRunToCompletion(t, ctx, postgres.SupplyChainImpactSupportsBackfill{DB: store, ChunkSize: 100})
	if _, err := db.ExecContext(ctx, `INSERT INTO supply_chain_impact_finding_supports_dirty (scope_id) VALUES ('scope-a')`); err != nil {
		t.Fatalf("mark the fence dirty: %v", err)
	}
	assertRefused("dirty old-writer fence")

	if _, err := db.ExecContext(ctx, `DELETE FROM supply_chain_impact_finding_supports_dirty`); err != nil {
		t.Fatalf("clear the fence: %v", err)
	}
	if got, err := fanOutLiveStore(db).FindRepositoryFindingHolders(ctx, "scope-a", []string{"R1"}); err != nil || len(got) == 0 {
		t.Fatalf("ready again: repository holders = %v, %v, want the holders", got, err)
	}
}
