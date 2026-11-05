// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10005 disposable PostgreSQL plan-shape guard for migration 280: on a bootstrapped database with many non-XRD content entities, the active Crossplane XRD text and its derived delta-base text are both served by fact_records_crossplane_xrd_entity_idx with no sequential scan of fact_records; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestCrossplaneXRDReadsUseThePartialIndexLive is the plan-shape guard for
// migration 280 (#10005). On a bootstrapped database with many non-XRD
// content entities, both the active XRD text and its derived delta-base text
// must be served by fact_records_crossplane_xrd_entity_idx, with no
// sequential scan of fact_records. A predicate drift between the reader and
// the index, or a dropped index, falls back to the scope-generation scan with
// a heap filter and fails here.
func TestCrossplaneXRDReadsUseThePartialIndexLive(t *testing.T) {
	f := openDeltaManifestFixtureWithin(t, 5*time.Minute)
	c := crossplaneDeltaCase{prefix: "plan"}
	for i := range 40 {
		scopeID := "plan-scope-" + strconv.Itoa(i)
		seedCrossplaneFull(f, scopeID, c.xrd("xrd-"+strconv.Itoa(i)))
		f.exec("seed non-XRD entities "+scopeID, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT $1 || ':e' || d, $1, $1 || '-full', 'content_entity', $1 || ':e' || d,
       'git', $1 || ':e' || d, clock_timestamp(), clock_timestamp(), FALSE,
       jsonb_build_object('relative_path', 'src/f' || d || '.go', 'entity_type', 'Function', 'entity_id', $1 || ':e' || d)
FROM generate_series(1, 500) AS d`, scopeID)
		if i < 10 {
			seedCrossplaneDelta(f, scopeID, []string{"src/f1.go"}, nil)
		}
	}
	f.exec("analyze", "ANALYZE fact_records")
	f.exec("analyze", "ANALYZE scope_generations")
	f.exec("analyze", "ANALYZE ingestion_scopes")

	base, err := deriveDeltaOverlayBaseQuery(listActiveCrossplaneXRDFactsQuery, crossplaneXRDDeltaOverlayScopeParam)
	if err != nil {
		t.Fatal(err)
	}
	// The base statement binds candidate base generation ids
	// (deltaOverlayBaseGenerations), not scope ids (#9611): each seeded
	// delta overlays its scope's activated c0 full generation, so the
	// EXPLAIN binds exactly the generation ids production binds.
	baseGenerations := make([]string, 0, 10)
	for i := range 10 {
		baseGenerations = append(baseGenerations, "plan-scope-"+strconv.Itoa(i)+"-full")
	}
	for name, run := range map[string]struct {
		query string
		args  []any
	}{
		"active": {listActiveCrossplaneXRDFactsQuery, []any{nil, "", listFactsByKindPageSize}},
		"base":   {base, []any{nil, "", listFactsByKindPageSize, baseGenerations}},
	} {
		var plan string
		if err := f.database.QueryRowContext(f.ctx, "EXPLAIN (FORMAT JSON) "+run.query, run.args...).Scan(&plan); err != nil {
			t.Fatalf("%s: EXPLAIN: %v", name, err)
		}
		if !strings.Contains(plan, `"Index Name": "`+crossplaneXRDIndexName+`"`) {
			t.Fatalf("%s XRD read does not use %s:\n%s", name, crossplaneXRDIndexName, plan)
		}
		if regexp.MustCompile(`"Node Type": "Seq Scan",[^}]*"Relation Name": "fact_records"`).MatchString(plan) {
			t.Fatalf("%s XRD read sequentially scans fact_records:\n%s", name, plan)
		}
	}
}
