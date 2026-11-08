// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// seedScopedFindingRow writes one active finding row with package identity
// for the seed-read proof.
func seedScopedFindingRow(t *testing.T, ctx context.Context, db *sql.DB, scope, generation, factID, cve, pkg, purl, name, eco string, tombstone bool) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'synthetic', $1, now(), now(), $5,
  jsonb_build_object('cve_id', $6::text, 'package_id', $7::text, 'purl', $8::text,
    'package_name', $9::text, 'ecosystem', $10::text))`,
		factID, scope, fanOutGen(scope, generation), facts.ReducerSupplyChainImpactFindingFactKind,
		tombstone, cve, pkg, purl, name, eco); err != nil {
		t.Fatalf("seed finding row %s: %v", factID, err)
	}
}

// TestListScopedFindingPackagesLive pins the #9779 seed read: distinct CVE
// and package identities of the generation's active finding rows for stamped
// CVEs only — never another CVE, a tombstoned copy, or another generation.
func TestListScopedFindingPackagesLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "seed-scope", "gen-2", "gen-1")
	seedScopedFindingRow(t, ctx, db, "seed-scope", "gen-2", "row-a", "CVE-2026-9001", "pkg-a", "pkg:npm/a@1", "a", "npm", false)
	seedScopedFindingRow(t, ctx, db, "seed-scope", "gen-2", "row-a2", "CVE-2026-9001", "pkg-a", "pkg:npm/a@1", "a", "npm", false)
	seedScopedFindingRow(t, ctx, db, "seed-scope", "gen-2", "row-b", "CVE-2026-9002", "pkg-b", "pkg:npm/b@2", "b", "npm", false)
	seedScopedFindingRow(t, ctx, db, "seed-scope", "gen-2", "row-tomb", "CVE-2026-9001", "pkg-t", "pkg:npm/t@1", "t", "npm", true)
	seedScopedFindingRow(t, ctx, db, "seed-scope", "gen-1", "row-old", "CVE-2026-9001", "pkg-old", "pkg:npm/old@1", "old", "npm", false)

	store := postgres.NewFactStore(postgres.SQLDB{DB: db})
	got, err := store.ListScopedSupplyChainImpactFindingPackages(ctx, "seed-scope", fanOutGen("seed-scope", "gen-2"), []string{"CVE-2026-9001"})
	if err != nil {
		t.Fatalf("ListScopedSupplyChainImpactFindingPackages() error = %v", err)
	}
	want := []supplychaincore.ScopedFindingPackage{{
		CVEID: "CVE-2026-9001", PackageID: "pkg-a", PURL: "pkg:npm/a@1", PackageName: "a", Ecosystem: "npm",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("seeds = %+v, want %+v (distinct, stamped CVE only, live generation only)", got, want)
	}
	if got, err := store.ListScopedSupplyChainImpactFindingPackages(ctx, "seed-scope", fanOutGen("seed-scope", "gen-2"), nil); err != nil || len(got) != 0 {
		t.Fatalf("empty stamp = %v err = %v, want no read", got, err)
	}
}
