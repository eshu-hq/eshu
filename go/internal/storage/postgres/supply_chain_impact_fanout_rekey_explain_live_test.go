// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestRekeyHolderLookupUsesAllThreeArmsLive is the criterion-9 proof for the
// widened holder lookup (#10408). At a 200-id lookup, with 90k finding rows
// on the table, the production query returns every scope holding a row
// that matches on any arm. Three arm-isolating id sets prove each arm fires
// through the shipped interface: CVE ids only reachable through cve_id,
// GHSA ids only reachable through advisory_id (no member set names them),
// and member ids only reachable through the stored advisory_group_members
// set (no cve_id or advisory_id field names them).
func TestRekeyHolderLookupUsesAllThreeArmsLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	for _, scope := range []string{"scope:10408:bulk-a", "scope:10408:bulk-b", "scope:10408:bulk-c"} {
		fanOutSeedScope(t, ctx, db, scope, "g1")
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'bulk:' || s.scope_id || ':' || g, s.scope_id, s.active_generation_id, $1, 'bulk:' || g, '1.0.0', 'synthetic',
  'reported', 'synthetic', 'bulk', now(), now(), false,
  jsonb_build_object(
    'cve_id', 'CVE-2026-' || lpad((g % 10000)::text, 4, '0'),
    'advisory_id', CASE WHEN g % 3 = 0 THEN 'GHSA-0401-' || lpad((g % 10000)::text, 4, '0')
                       ELSE 'CVE-2026-' || lpad((g % 10000)::text, 4, '0') END,
    'impact_status', 'active',
    'provenance', CASE WHEN g % 2 = 0
      THEN jsonb_build_object('advisory_group_members',
             to_jsonb(ARRAY['CVE-2026-' || lpad((g % 10000)::text, 4, '0'),
                            'GHSA-0401-' || lpad((g % 10000)::text, 4, '0')]))
      ELSE '{}'::jsonb END)
FROM (SELECT scope_id, active_generation_id FROM ingestion_scopes WHERE scope_id LIKE 'scope:10408:bulk%') s
CROSS JOIN generate_series(1, 30000) g`, facts.ReducerSupplyChainImpactFindingFactKind); err != nil {
		t.Fatalf("seed bulk findings: %v", err)
	}
	if _, err := db.ExecContext(ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	store := postgres.SupplyChainImpactFanOutStore{
		Beginner: postgres.SQLDB{DB: db}, DB: postgres.SQLDB{DB: db}, LeaseOwner: fanOutLiveOwner,
	}
	// Odd ids name no member set; odd multiples of 3 carry the GHSA advisory
	// with an empty member set; even non-multiples of 3 name the GHSA id only
	// inside advisory_group_members.
	cveIDs := make([]string, 0, 50)
	for i := 1; i <= 50; i++ {
		cveIDs = append(cveIDs, fmt.Sprintf("CVE-2026-%04d", 2*i-1))
	}
	advisoryIDs := make([]string, 0, 30)
	for i := 1; i <= 30; i++ {
		advisoryIDs = append(advisoryIDs, fmt.Sprintf("GHSA-0401-%04d", 6*i-3))
	}
	memberIDs := make([]string, 0, 20)
	for i := 1; i <= 20; i++ {
		memberIDs = append(memberIDs, fmt.Sprintf("GHSA-0401-%04d", 6*i-4))
	}
	for name, ids := range map[string][]string{"cve": cveIDs, "advisory": advisoryIDs, "member": memberIDs} {
		got, err := store.FindHolderScopes(ctx, "scope:10408:bulk-a", ids)
		if err != nil {
			t.Fatalf("FindHolderScopes(%s): %v", name, err)
		}
		if len(got) != 2 || got[0] != "scope:10408:bulk-b" || got[1] != "scope:10408:bulk-c" {
			t.Fatalf("FindHolderScopes(%s) = %v, want both holder scopes", name, got)
		}
	}

	// The full 200-id lookup through the shipped constant: every arm
	// fires at once. (The handler chunks wider lookups into 25-id
	// statements, #10564 H; the store runs one statement per call.)
	ids := append(append(append([]string{}, cveIDs...), advisoryIDs...), memberIDs...)
	for i := 1; i <= 100; i++ {
		ids = append(ids, fmt.Sprintf("NOPE-0401-%04d", i))
	}
	if len(ids) != 200 {
		t.Fatalf("lookup set has %d ids, want the 200-id lookup", len(ids))
	}
	got, err := store.FindHolderScopes(ctx, "scope:10408:bulk-a", ids)
	if err != nil {
		t.Fatalf("FindHolderScopes: %v", err)
	}
	if len(got) != 2 || got[0] != "scope:10408:bulk-b" || got[1] != "scope:10408:bulk-c" {
		t.Fatalf("FindHolderScopes = %v, want both holder scopes", got)
	}
	rows, err := db.QueryContext(ctx, `EXPLAIN (ANALYZE, BUFFERS) `+postgres.FindSupplyChainImpactHoldersQuery, ids, "scope:10408:bulk-a")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	t.Logf("holder lookup plan at the 200-id bound, 90k finding rows:\n%s", strings.Join(plan, "\n"))

	// Each btree arm on the shipped constant at a single id: the planner
	// must serve it from its own partial index. The cve arm rides migration
	// 283's table-wide expression statistics; the advisory arm rides
	// migration 290's (the same pattern). The member-set GIN arm is proven
	// behaviorally above; its selection stays scale-dependent.
	for _, tc := range []struct {
		name  string
		id    string
		index string
	}{
		{"cve", "CVE-2026-0001", "fact_records_supply_chain_impact_lookup_idx"},
		{"advisory", "GHSA-0401-0003", "fact_records_supply_chain_impact_advisory_lookup_idx"},
	} {
		rows, err := db.QueryContext(ctx, `EXPLAIN (ANALYZE, BUFFERS) `+postgres.FindSupplyChainImpactHoldersQuery, []string{tc.id}, "scope:10408:bulk-a")
		if err != nil {
			t.Fatalf("explain %s: %v", tc.name, err)
		}
		var single []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			single = append(single, line)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("read plan %s: %v", tc.name, err)
		}
		text := strings.Join(single, "\n")
		t.Logf("holder lookup single-%s plan:\n%s", tc.name, text)
		if !strings.Contains(text, tc.index) {
			t.Fatalf("single-%s plan does not use %s:\n%s", tc.name, tc.index, text)
		}
		if strings.Contains(text, "Seq Scan on fact_records") {
			t.Fatalf("single-%s plan sequentially scans fact_records:\n%s", tc.name, text)
		}
	}
}
