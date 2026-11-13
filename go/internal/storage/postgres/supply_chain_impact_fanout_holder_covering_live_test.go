// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// holderCoveringCTEHead is the byte-prefix the arm derivation below
// requires of the shipped constant, so a change to the query shape fails
// here instead of silently deriving the wrong arms.
const holderCoveringCTEHead = "WITH candidates AS MATERIALIZED ("

// holderCoveringArmSeparator splits the three lookup arms inside the
// MATERIALIZED candidate set.
const holderCoveringArmSeparator = "\n    UNION\n"

// holderCoveringArmsEnd marks where the candidate set ends and the outer
// scope join begins.
const holderCoveringArmsEnd = "\n)\nSELECT DISTINCT candidates.scope_id"

// TestHolderCoveringExpressionBlocksIndexOnlyLive is the #10643 mechanism
// guard for the H-3 no-build verdict. It builds the prescribed covering
// shape under test-local names on a hot-CVE/advisory fixture, then EXPLAINs
// the CVE and advisory arms derived byte-identical from the shipped
// FindSupplyChainImpactHoldersQuery constant. The planner serves both arms
// from the covering shape yet keeps every candidate's heap fetch: no
// index-only path is generated for a query that filters on an indexed
// expression ((payload->>'cve_id') = ANY(...)), on PostgreSQL 18.6 with a
// fully visible heap. If a future Postgres (or a query rewrite) starts
// generating index-only arms, the no-index-only assertions below fail and
// point at re-pricing H-3 — that failure is the re-trigger firing.
func TestHolderCoveringExpressionBlocksIndexOnlyLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	const (
		ownScope    = "h3cov:own"
		tombScope   = "h3cov:tomb-only"
		hotCVE      = "CVE-2026-9001"
		hotAdvisory = "GHSA-h3cov-9001"
	)
	holders := []string{"h3cov:hold-a", "h3cov:hold-b", "h3cov:hold-c", "h3cov:hold-d", "h3cov:hold-e"}
	fanOutSeedScope(t, ctx, db, ownScope, "g1")
	fanOutSeedScope(t, ctx, db, tombScope, "g1")
	for _, scope := range holders {
		fanOutSeedScope(t, ctx, db, scope, "g1")
	}

	// Bulk background: distinct ids per row so only the hot ids
	// concentrate. Rows carry production-like widths (the scope widths and
	// a 2 KB note) so the cost model prices heap fetches honestly.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'h3cov:bg:' || scopes.scope_id || ':' || g, scopes.scope_id, scopes.active_generation_id, $1,
  'h3cov:bg:' || g, '1.0.0', 'synthetic', 'reported', 'synthetic', 'h3cov:bg', now(), now(), false,
  jsonb_build_object(
    'cve_id', 'CVE-2026-' || lpad((g % 40000)::text, 5, '0'),
    'advisory_id', 'GHSA-h3cov-' || lpad((g % 40000)::text, 5, '0'),
    'impact_status', CASE WHEN g % 7 = 0 THEN 'suppressed' ELSE 'active' END,
    'note', repeat('x', 2000),
    'provenance', '{}'::jsonb)
FROM (SELECT scope_id, active_generation_id FROM ingestion_scopes WHERE scope_id LIKE 'h3cov:%') scopes
CROSS JOIN generate_series(1, 8000) g`, facts.ReducerSupplyChainImpactFindingFactKind); err != nil {
		t.Fatalf("seed background findings: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'h3cov:hot-cve:' || scopes.scope_id || ':' || g, scopes.scope_id, scopes.active_generation_id, $1,
  'h3cov:hot-cve:' || g, '1.0.0', 'synthetic', 'reported', 'synthetic', 'h3cov:hot', now(), now(), false,
  jsonb_build_object(
    'cve_id', $2::text,
    'advisory_id', 'GHSA-h3cov-distinct-' || lpad(g::text, 5, '0'),
    'impact_status', 'active',
    'note', repeat('x', 2000),
    'provenance', '{}'::jsonb)
FROM (SELECT scope_id, active_generation_id FROM ingestion_scopes WHERE scope_id LIKE 'h3cov:hold-%') scopes
CROSS JOIN generate_series(1, 30) g`, facts.ReducerSupplyChainImpactFindingFactKind, hotCVE); err != nil {
		t.Fatalf("seed hot CVE findings: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'h3cov:hot-adv:' || scopes.scope_id || ':' || g, scopes.scope_id, scopes.active_generation_id, $1,
  'h3cov:hot-adv:' || g, '1.0.0', 'synthetic', 'reported', 'synthetic', 'h3cov:hot', now(), now(), false,
  jsonb_build_object(
    'cve_id', 'CVE-2026-distinct-' || lpad(g::text, 5, '0'),
    'advisory_id', $2::text,
    'impact_status', 'active',
    'note', repeat('x', 2000),
    'provenance', '{}'::jsonb)
FROM (SELECT scope_id, active_generation_id FROM ingestion_scopes WHERE scope_id LIKE 'h3cov:hold-%') scopes
CROSS JOIN generate_series(1, 30) g`, facts.ReducerSupplyChainImpactFindingFactKind, hotAdvisory); err != nil {
		t.Fatalf("seed hot advisory findings: %v", err)
	}
	// Exclusion edges: own-scope live rows, tombstoned hot rows, and a hot
	// row with no cve key (NULL handling must match on both sides).
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'h3cov:edge:' || s.scope_id || ':' || g, s.scope_id, s.active_generation_id, $1,
  'h3cov:edge:' || g, '1.0.0', 'synthetic', 'reported', 'synthetic', 'h3cov:edge', now(), now(),
  s.scope_id = $3,
  CASE WHEN g = 3 THEN jsonb_build_object('advisory_id', $4::text, 'impact_status', 'active')
       ELSE jsonb_build_object('cve_id', $2::text, 'advisory_id', $4::text, 'impact_status', 'active') END
FROM (SELECT scope_id, active_generation_id FROM ingestion_scopes
      WHERE scope_id IN ($5, $3)) s
CROSS JOIN generate_series(1, 3) g`, facts.ReducerSupplyChainImpactFindingFactKind,
		hotCVE, tombScope, hotAdvisory, ownScope); err != nil {
		t.Fatalf("seed exclusion edges: %v", err)
	}

	// The prescribed H-3 shape under test-local names: disposable database,
	// so a plain build is fast and never touches a shipped migration.
	for _, ddl := range []string{
		`CREATE INDEX h3cov_guard_cve_scope_gen_idx ON fact_records ((payload->>'cve_id'), scope_id, generation_id)
		 WHERE fact_kind = 'reducer_supply_chain_impact_finding' AND is_tombstone = FALSE`,
		`CREATE INDEX h3cov_guard_adv_scope_gen_idx ON fact_records ((payload->>'advisory_id'), scope_id, generation_id)
		 WHERE fact_kind = 'reducer_supply_chain_impact_finding' AND is_tombstone = FALSE`,
	} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			t.Fatalf("build guard covering index: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `VACUUM (ANALYZE) fact_records`); err != nil {
		t.Fatalf("vacuum analyze: %v", err)
	}

	arms := holderCoveringDerivedArms(t, postgres.FindSupplyChainImpactHoldersQuery)
	for _, tc := range []struct {
		name  string
		arm   string
		id    string
		index string
	}{
		{"cve", arms[0], hotCVE, "h3cov_guard_cve_scope_gen_idx"},
		{"advisory", arms[1], hotAdvisory, "h3cov_guard_adv_scope_gen_idx"},
	} {
		plan := explainQuery(t, ctx, db, `EXPLAIN (ANALYZE, BUFFERS) `+tc.arm, []string{tc.id})
		t.Logf("holder single-%s arm with the covering shape available:\n%s", tc.name, plan)
		if !strings.Contains(plan, "Bitmap Index Scan on "+tc.index) {
			t.Fatalf("single-%s arm does not serve from the covering shape %s:\n%s", tc.name, tc.index, plan)
		}
		if strings.Contains(plan, "Index Only Scan") {
			t.Fatalf("single-%s arm went index-only: the expression mechanism changed, re-price H-3:\n%s",
				tc.name, plan)
		}
		if !strings.Contains(plan, "Heap Blocks: exact=") {
			t.Fatalf("single-%s arm reports no heap blocks: expected heap fetches:\n%s", tc.name, plan)
		}
	}

	// The covering shape moves no rows: the shipped query returns the same
	// ordered scope set as the frozen H0 baseline.
	got := queryScopes(t, ctx, db, postgres.FindSupplyChainImpactHoldersQuery, []string{hotCVE, hotAdvisory}, ownScope)
	want := queryScopes(t, ctx, db, holderH0BaselineQuery, []string{hotCVE, hotAdvisory}, ownScope)
	if !slices.Equal(got, want) {
		t.Fatalf("scope sets differ: shipped=%v h0=%v", got, want)
	}
	if len(got) != len(holders) {
		t.Fatalf("holder scopes = %v, want the %d holder scopes", got, len(holders))
	}
	t.Logf("holder covering guard: %d scopes identical shipped/H0 with the covering shape present: %v",
		len(got), got)
}

// holderCoveringDerivedArms derives the three lookup arms from the shipped
// holder query constant, so the guard cannot drift from production SQL: the
// CTE head must be a byte-prefix of the constant and the arm separator and
// the outer-query tail must match exactly where the derivation cuts.
func holderCoveringDerivedArms(t *testing.T, query string) [3]string {
	t.Helper()
	trimmed := strings.TrimSpace(query)
	if !strings.HasPrefix(trimmed, holderCoveringCTEHead) {
		t.Fatal("derived arms are not a byte-prefix of the shipped constant")
	}
	rest := trimmed[len(holderCoveringCTEHead):]
	tail := strings.Index(rest, holderCoveringArmsEnd)
	if tail < 0 {
		t.Fatal("outer-query tail marker not found in the shipped constant")
	}
	parts := strings.Split(rest[:tail], holderCoveringArmSeparator)
	if len(parts) != 3 {
		t.Fatalf("derived %d arms, want the 3 lookup arms", len(parts))
	}
	return [3]string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])}
}

// holderQueryRunner is the QueryContext surface shared by *sql.DB and
// *sql.Tx.
type holderQueryRunner interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// explainQuery runs an EXPLAIN statement and returns the joined plan lines.
func explainQuery(t *testing.T, ctx context.Context, querier holderQueryRunner, stmt string, args ...any) string {
	t.Helper()
	rows, err := querier.QueryContext(ctx, stmt, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	return strings.Join(plan, "\n")
}

// queryScopes runs a two-argument (ids, ownScope) holder query and returns
// the ordered scope ids.
func queryScopes(t *testing.T, ctx context.Context, querier holderQueryRunner, query string, ids []string, ownScope string) []string {
	t.Helper()
	rows, err := querier.QueryContext(ctx, query, ids, ownScope)
	if err != nil {
		t.Fatalf("holder query %v: %v", ids, err)
	}
	var out []string
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		out = append(out, scope)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("read holder rows: %v", err)
	}
	return out
}
