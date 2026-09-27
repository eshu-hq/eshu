// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// badInnerChildren are the inner-side node types that make a Nested Loop
// rescan work per outer row: the class the G8 stall belongs to (#7127
// arbiter ruling arb-7127-g8, P1).
var badInnerChildren = map[string]bool{
	"CTE Scan": true, "Seq Scan": true, "Function Scan": true, "Subquery Scan": true, "Materialize": true,
}

func decodePlan(t *testing.T, raw string) map[string]any {
	t.Helper()
	var doc []map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	return doc[0]["Plan"].(map[string]any)
}

func walkPlan(node map[string]any, visit func(map[string]any)) {
	visit(node)
	children, _ := node["Plans"].([]any)
	for _, child := range children {
		if m, ok := child.(map[string]any); ok {
			walkPlan(m, visit)
		}
	}
}

func subplan(root map[string]any, name string) map[string]any {
	var found map[string]any
	walkPlan(root, func(n map[string]any) {
		if found == nil && n["Subplan Name"] == name {
			found = n
		}
	})
	return found
}

// stateScans returns the nodes that read changed_since_key_state under node,
// excluding ModifyTable (which names its target but reads nothing).
func stateScans(node map[string]any) []map[string]any {
	var out []map[string]any
	if node == nil {
		return nil
	}
	walkPlan(node, func(n map[string]any) {
		if n["Relation Name"] == "changed_since_key_state" && n["Node Type"] != "ModifyTable" {
			out = append(out, n)
		}
	})
	return out
}

// planClassFailures lists how a plan of the incremental statement breaks the
// P1 class: (a) del must read the state table by Tid Scan; (b) no Nested
// Loop may have a rescanning inner child; (c) diff's state side must use the
// primary key by scope_id. It also returns the diff state-side row estimate.
//
// With allowSeqState a sequential scan of the state side also passes (c): an
// over-estimate may choose it, which is O(fleet) and bounded (the arbiter's
// audit table, and ruling 8.4 for the largest scope).
func planClassFailures(t *testing.T, raw string, allowSeqState bool) (failures []string, stateRows float64) {
	t.Helper()
	root := decodePlan(t, raw)
	del := subplan(root, "CTE del")
	if del == nil {
		failures = append(failures, "(a) no CTE del in the plan")
	}
	for _, n := range stateScans(del) {
		if n["Node Type"] != "Tid Scan" {
			failures = append(failures, fmt.Sprintf("(a) del reads the state table by %v, want Tid Scan", n["Node Type"]))
		}
	}
	walkPlan(root, func(n map[string]any) {
		children, _ := n["Plans"].([]any)
		if n["Node Type"] != "Nested Loop" || len(children) < 2 {
			return
		}
		inner, _ := children[1].(map[string]any)
		if inner != nil && badInnerChildren[str(inner["Node Type"])] {
			failures = append(failures, fmt.Sprintf("(b) Nested Loop (%v) with inner %v %v", n["Join Type"], inner["Node Type"], inner["CTE Name"]))
		}
	})
	diff := subplan(root, "CTE diff")
	scans := stateScans(diff)
	if len(scans) == 0 {
		failures = append(failures, "(c) diff does not read the state table")
	}
	for _, n := range scans {
		stateRows, _ = n["Plan Rows"].(float64)
		index := str(n["Index Name"])
		cond := str(n["Index Cond"])
		if n["Node Type"] == "Bitmap Heap Scan" {
			if kids, _ := n["Plans"].([]any); len(kids) > 0 {
				k := kids[0].(map[string]any)
				index, cond = str(k["Index Name"]), str(k["Index Cond"])
			}
		}
		if allowSeqState && n["Node Type"] == "Seq Scan" {
			continue
		}
		if index != "changed_since_key_state_pkey" || !strings.Contains(cond, "scope_id") {
			failures = append(failures, fmt.Sprintf("(c) diff reads the state table by %v %q cond %q", n["Node Type"], index, cond))
		}
	}
	return failures, stateRows
}

// explainIncremental plans statement for scope at the link's settings.
func (l *ledgerDB) explainIncremental(t *testing.T, statement, scopeID string) string {
	t.Helper()
	tx, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, s := range []string{`SET LOCAL work_mem = '256MB'`, `SET LOCAL plan_cache_mode = force_custom_plan`} {
		if _, err := tx.ExecContext(l.ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	var plan string
	if err := tx.QueryRowContext(l.ctx, `EXPLAIN (FORMAT JSON) `+statement,
		scopeID, scopeID+"-g1", scopeID+"-g0", linksfreshnessstore.DigestVersion, time.Now()).Scan(&plan); err != nil {
		t.Fatalf("explain: %v", err)
	}
	return plan
}

// seedRekeyScope seeds keys content entities in g0 and, in g1, drops the
// first drop keys and adds as many new ones, then roots the scope at g0.
func (l *ledgerDB) seedRekeyScope(t *testing.T, w *linksfreshnessstore.LinkWriter, scopeID string, keys, drop int) {
	t.Helper()
	l.seedScope(t, scopeID)
	l.seedGeneration(t, scopeID, scopeID+"-g0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
	l.seedGeneration(t, scopeID, scopeID+"-g1", false, "active", fixtureEpoch.Add(time.Hour), time.Time{})
	l.seedBulkGeneration(t, scopeID, scopeID+"-g0", keys, 0)
	l.exec(t, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, source_uri, observed_at, ingested_at, is_tombstone, payload)
SELECT $2 || '/' || i, $1, $2, 'content_entity', 'ent:' || i, 'git', 'ent:' || i, 'f' || (i / 50) || '.go',
       $3, $3, FALSE, jsonb_build_object('n', i, 'v', 0, 'indexed_at', $2, 'body', repeat('x', 200))
FROM generate_series($4::int + 1, $5::int + $4::int) AS i`, scopeID, scopeID+"-g1", fixtureEpoch, drop, keys)
	l.journal(t, scopeID, scopeID+"-g0", "")
	l.journal(t, scopeID, scopeID+"-g1", scopeID+"-g0")
	if res := mustLink(t, w, l, scopeID); res.Kind != linksfreshnessstore.LinkKindRoot {
		t.Fatalf("root %s = %+v", scopeID, res)
	}
}

// statsStates plants the four statistics states of P1 for one scope.
var statsStates = []string{"absent", "fresh", "never", "over"}

func (l *ledgerDB) plantStats(t *testing.T, state, scopeID string, keys int) {
	t.Helper()
	switch state {
	case "fresh":
		l.exec(t, `ANALYZE changed_since_key_state`)
	case "absent":
		l.exec(t, `CREATE TABLE p1_saved AS SELECT * FROM changed_since_key_state WHERE scope_id = $1`, scopeID)
		l.exec(t, `DELETE FROM changed_since_key_state WHERE scope_id = $1`, scopeID)
		l.exec(t, `ANALYZE changed_since_key_state`)
		l.exec(t, `INSERT INTO changed_since_key_state SELECT * FROM p1_saved`)
		l.exec(t, `DROP TABLE p1_saved`)
	case "never":
		l.exec(t, `SELECT pg_clear_relation_stats('public', 'changed_since_key_state')`)
		for _, column := range []string{"scope_id", "fact_category", "stable_fact_key", "fact_kind", "state", "owner_uri"} {
			l.exec(t, `SELECT pg_clear_attribute_stats('public', 'changed_since_key_state', $1, false)`, column)
		}
	case "over":
		l.exec(t, `
INSERT INTO changed_since_key_state (scope_id, fact_category, stable_fact_key, fact_kind, state)
SELECT $1, 'facts', 'junk:' || i, 'junk', sha256(('j' || i)::bytea) FROM generate_series(1, $2::int) AS i`, scopeID, 2*keys)
		l.exec(t, `ANALYZE changed_since_key_state`)
		l.exec(t, `DELETE FROM changed_since_key_state WHERE scope_id = $1 AND fact_category = 'facts' AND stable_fact_key LIKE 'junk:%'`, scopeID)
	}
}

// formI is the executor's rejected candidate (i): the ruled ctid delete with
// the scope predicate kept on the target. Derived from the shipped statement
// so it cannot drift from it.
func formI(t *testing.T) string {
	t.Helper()
	const anchor = "WHERE t.ctid = ANY"
	if !strings.Contains(linksfreshnessstore.IncrementalLinkSQL, anchor) {
		return ""
	}
	return strings.Replace(linksfreshnessstore.IncrementalLinkSQL, anchor, "WHERE t.scope_id = $1 AND t.ctid = ANY", 1)
}

// TestIncrementalLinkPlanClassUnderPlantedStatistics is P1 of arbiter ruling
// arb-7127-g8: under four statistics states and two scope sizes, in a fleet
// of fewer than 100 scopes (20 filler scopes of 20,000 rows), the
// incremental statement deletes by Tid Scan,
// never plans a rescanning nested loop, and reads the state side by the
// primary key. The shipped statement and the rejected form (i) are its RED.
func TestIncrementalLinkPlanClassUnderPlantedStatistics(t *testing.T) {
	l := openLedgerDB(t)
	l.exec(t, `ALTER TABLE changed_since_key_state SET (autovacuum_enabled = false)`)
	t.Cleanup(func() { _, _ = l.raw.ExecContext(l.ctx, `ANALYZE changed_since_key_state`) })
	for i := range 20 {
		l.exec(t, `
INSERT INTO changed_since_key_state (scope_id, fact_category, stable_fact_key, fact_kind, state)
SELECT $1, 'content_entities', 'ent:' || k, 'content_entity', sha256(($1 || k)::bytea)
FROM generate_series(1, 20000) AS k`, fmt.Sprintf("filler-%02d", i))
	}
	w := linksfreshnessstore.NewLinkWriter(l.store)
	sizes := map[string]int{"s600": 600, "s45k": 45000}
	for scopeID, keys := range sizes {
		l.seedRekeyScope(t, w, scopeID, keys, keys/10)
	}

	for _, scopeID := range []string{"s600", "s45k"} {
		for _, state := range statsStates {
			l.plantStats(t, state, scopeID, sizes[scopeID])
			label := scopeID + "/" + state
			failures, rows := planClassFailures(t, l.explainIncremental(t, linksfreshnessstore.IncrementalLinkSQL, scopeID), state == "over")
			if state == "absent" && rows > 1 {
				t.Fatalf("%s: precondition not met, state side estimated at %.0f rows (want at most 1); the test cannot fail", label, rows)
			}
			if len(failures) > 0 {
				t.Errorf("%s: shipped IncrementalLinkSQL breaks the plan class: %s", label, strings.Join(failures, "; "))
			} else {
				t.Logf("%s: plan class holds (state side estimated at %.0f rows)", label, rows)
			}
			if state != "absent" {
				continue
			}
			// RED: the frozen pre-fix statement and form (i) must fail.
			red, _ := planClassFailures(t, l.explainIncremental(t, shippedIncrementalLinkSQL, scopeID), false)
			if !hasPrefix(red, "(b)") {
				t.Errorf("%s: the frozen shipped statement passed (b); the gate cannot see the stall plan: %v", label, red)
			}
			if f := formI(t); f != "" {
				redI, _ := planClassFailures(t, l.explainIncremental(t, f, scopeID), false)
				if !hasPrefix(redI, "(a)") {
					t.Errorf("%s: form (i) passed (a); the gate cannot see its scope scan: %v", label, redI)
				}
			}
		}
	}
}

func hasPrefix(items []string, prefix string) bool {
	for _, item := range items {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}
