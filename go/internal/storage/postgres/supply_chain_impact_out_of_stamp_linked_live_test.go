// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The fact store must keep the linked out-of-stamp probe the handler asserts
// at runtime (#10972 D2): without it the handler never forces the rekey reap.
var _ interface {
	HasLinkedOutOfStampActiveSupplyChainImpactRows(ctx context.Context, scopeID, generationID string, ids []string) (bool, error)
} = postgres.FactStore{}

// seedLinkedFindingRow writes one active finding row with an advisory id and
// an advisory group member set, then settles the supports table the probe
// reads (#11197).
func seedLinkedFindingRow(t *testing.T, ctx context.Context, db *sql.DB, scope, generation, factID, cve, advisory string, members []string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'synthetic', $1, now(), now(), false,
  jsonb_build_object('cve_id', $5::text, 'advisory_id', $6::text, 'package_id', 'pkg-' || $1, 'purl', 'pkg:npm/' || $1 || '@1',
    'package_name', $1, 'ecosystem', 'npm', 'provenance', jsonb_build_object('advisory_group_members', to_jsonb($7::text[]))))`,
		factID, scope, fanOutGen(scope, generation), facts.ReducerSupplyChainImpactFindingFactKind, cve, advisory, members); err != nil {
		t.Fatalf("seed finding row %s: %v", factID, err)
	}
	settleSupplyChainImpactSupports(t, ctx, db)
}

// TestSupplyChainImpactSupportsLinkedOutOfStampProbeArmsLive pins the shipped linked
// probe (hasLinkedOutOfStampActiveSupplyChainImpactRowsQuery, run through the
// FactStore method): a row outside the stamp is linked through its advisory id
// or its advisory group members, a GHSA-only row with no CVE id counts, an
// unlinked row outside the stamp does not, and neither does a linked row whose
// CVE id the stamp already names.
func TestSupplyChainImpactSupportsLinkedOutOfStampProbeArmsLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := postgres.NewFactStore(postgres.SQLDB{DB: db})
	cases := []struct {
		name, scope string
		seed        func(scope string)
		stamp       []string
		want        bool
	}{
		{"advisory arm", "linked-advisory", func(s string) {
			seedLinkedFindingRow(t, ctx, db, s, "g1", s+"-row", "CVE-2026-7400", "GHSA-7400-aaaa", []string{"GHSA-7400-aaaa"})
		}, []string{"GHSA-7400-aaaa"}, true},
		{"members arm", "linked-members", func(s string) {
			seedLinkedFindingRow(t, ctx, db, s, "g1", s+"-row", "CVE-2026-7401", "GHSA-7401-aaaa", []string{"GHSA-7401-aaaa", "GHSA-7401-bbbb"})
		}, []string{"GHSA-7401-bbbb"}, true},
		{"no CVE id, members arm", "linked-empty-cve", func(s string) {
			seedLinkedFindingRow(t, ctx, db, s, "g1", s+"-row", "", "GHSA-7402-aaaa", []string{"GHSA-7402-aaaa", "GHSA-7402-bbbb"})
		}, []string{"GHSA-7402-bbbb"}, true},
		{"unlinked row outside the stamp", "linked-none", func(s string) {
			seedLinkedFindingRow(t, ctx, db, s, "g1", s+"-row", "CVE-2026-7403", "CVE-2026-7403", []string{"CVE-2026-7403"})
		}, []string{"CVE-2026-7499"}, false},
		{"linked row inside the stamp", "linked-inside", func(s string) {
			seedLinkedFindingRow(t, ctx, db, s, "g1", s+"-row", "CVE-2026-7404", "GHSA-7404-aaaa", []string{"GHSA-7404-aaaa", "CVE-2026-7404"})
		}, []string{"CVE-2026-7404", "GHSA-7404-aaaa"}, false},
	}
	for _, tc := range cases {
		fanOutSeedScope(t, ctx, db, tc.scope, "g1")
		tc.seed(tc.scope)
		var members sql.NullString
		if err := db.QueryRowContext(ctx, `
SELECT (payload #> '{provenance,advisory_group_members}')::text FROM supply_chain_impact_finding_supports WHERE scope_id = $1`, tc.scope).Scan(&members); err != nil || !members.Valid {
			t.Fatalf("%s: fixture: no settled support with group members (%v, %v)", tc.name, members, err)
		}
		has, err := store.HasLinkedOutOfStampActiveSupplyChainImpactRows(ctx, tc.scope, fanOutGen(tc.scope, "g1"), tc.stamp)
		if err != nil {
			t.Fatalf("%s: probe error = %v", tc.name, err)
		}
		if has != tc.want {
			t.Errorf("%s: linked probe = %v, want %v (stamp %v, support members %s)", tc.name, has, tc.want, tc.stamp, members.String)
		}
	}
	if has, err := store.HasLinkedOutOfStampActiveSupplyChainImpactRows(ctx, "linked-advisory", fanOutGen("linked-advisory", "g1"), nil); err != nil || has {
		t.Errorf("empty stamp probe = %v err = %v, want false without a read", has, err)
	}
}
