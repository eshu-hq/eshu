// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"encoding/json"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// nestedLoopsOverCTE lists the Nested Loop nodes of an EXPLAIN (FORMAT JSON)
// plan whose inner (second) child scans the named CTE: the shape that
// rescans the whole CTE once per outer row.
func nestedLoopsOverCTE(t *testing.T, raw, cte string) []string {
	t.Helper()
	var doc []map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	var found []string
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		children, _ := node["Plans"].([]any)
		if node["Node Type"] == "Nested Loop" && len(children) == 2 {
			inner, _ := children[1].(map[string]any)
			for inner != nil && inner["Node Type"] == "Materialize" {
				grand, _ := inner["Plans"].([]any)
				if len(grand) == 0 {
					break
				}
				inner, _ = grand[0].(map[string]any)
			}
			if inner != nil && inner["Node Type"] == "CTE Scan" && inner["CTE Name"] == cte {
				outer, _ := children[0].(map[string]any)
				found = append(found, "Nested Loop: outer "+str(outer["Node Type"])+" on "+str(outer["Relation Name"])+
					", inner CTE Scan on "+cte)
			}
		}
		for _, child := range children {
			if m, ok := child.(map[string]any); ok {
				walk(m)
			}
		}
	}
	walk(doc[0]["Plan"].(map[string]any))
	return found
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// TestIncrementalLinkSurvivesStaleScopeStatistics is the RED test of the G8
// stall (#7127 PR-3a evidence, "G8 stall diagnosis"): when the state-table
// statistics were taken while a scope had no state rows, the planner
// estimates that scope at about one row. The incremental statement must still
// never plan a Nested Loop that rescans the diff CTE per state row, and the
// link must finish within a bound.
func TestIncrementalLinkSurvivesStaleScopeStatistics(t *testing.T) {
	l := openLedgerDB(t)
	l.exec(t, `ALTER TABLE changed_since_key_state SET (autovacuum_enabled = false)`)
	const scope, keys, removed = "stale", 60000, 2000
	l.seedScope(t, scope)
	l.seedGeneration(t, scope, scope+"-g0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
	l.seedGeneration(t, scope, scope+"-g1", false, "active", fixtureEpoch.Add(time.Hour), time.Time{})
	l.seedBulkGeneration(t, scope, scope+"-g0", keys, 0)
	// g1 drops the first `removed` keys, so the link deletes that many state rows.
	l.exec(t, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, source_uri, observed_at, ingested_at, is_tombstone, payload)
SELECT $2 || '/' || i, $1, $2, 'content_entity', 'ent:' || i, 'git', 'ent:' || i, 'f' || (i / 50) || '.go',
       $4, $4, FALSE, jsonb_build_object('n', i, 'v', CASE WHEN i % 97 = 0 THEN 1 ELSE 0 END, 'body', repeat('x', 200))
FROM generate_series($3::int + 1, $5::int) AS i`, scope, scope+"-g1", removed, fixtureEpoch, keys)
	l.journal(t, scope, scope+"-g0", "")
	l.journal(t, scope, scope+"-g1", scope+"-g0")
	// Another scope keeps the table's statistics populated.
	l.exec(t, `
INSERT INTO changed_since_key_state (scope_id, fact_category, stable_fact_key, fact_kind, state)
SELECT 'filler', 'content_entities', 'ent:' || k, 'content_entity', sha256(('f' || k)::bytea)
FROM generate_series(1, 20000) AS k`)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	if res := mustLink(t, w, l, scope); res.Kind != linksfreshnessstore.LinkKindRoot {
		t.Fatalf("root = %+v", res)
	}
	// Plant stale statistics: analyze while the scope has no state rows,
	// then restore them, the shape an autoanalyze taken mid re-root leaves.
	l.exec(t, `CREATE TABLE stale_saved AS SELECT * FROM changed_since_key_state WHERE scope_id = 'stale'`)
	l.exec(t, `DELETE FROM changed_since_key_state WHERE scope_id = 'stale'`)
	l.exec(t, `ANALYZE changed_since_key_state`)
	l.exec(t, `INSERT INTO changed_since_key_state SELECT * FROM stale_saved`)

	tx, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for _, s := range []string{`SET LOCAL work_mem = '256MB'`, `SET LOCAL plan_cache_mode = force_custom_plan`} {
		if _, err := tx.ExecContext(l.ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	var plan string
	if err := tx.QueryRowContext(l.ctx, `EXPLAIN (FORMAT JSON) `+linksfreshnessstore.IncrementalLinkSQL,
		scope, scope+"-g1", scope+"-g0", linksfreshnessstore.DigestVersion, time.Now()).Scan(&plan); err != nil {
		t.Fatalf("explain: %v", err)
	}
	_ = tx.Rollback()
	if loops := nestedLoopsOverCTE(t, plan, "diff"); len(loops) > 0 {
		t.Errorf("stale statistics plan the incremental link with %v; it rescans diff once per state row", loops)
	}

	// The plan-shape assertion above is the primary signal. The wall bound is
	// a secondary guard sized for a shared host: the fixed link measured
	// 194 ms here and the stalled plan 12.7 s, so 5 s sits a factor of 25
	// above the one and 2.5 below the other.
	const bound = 5 * time.Second
	w.StatementTimeout = 30 * time.Second
	res, err := w.LinkNext(l.ctx, scope)
	if err != nil {
		t.Fatalf("incremental link under stale statistics failed: %v", err)
	}
	if res.Kind != linksfreshnessstore.LinkKindIncremental {
		t.Fatalf("link = %+v, want incremental", res)
	}
	t.Logf("incremental link under stale statistics: %s, %d delta rows", res.Duration, res.DeltaRows)
	if res.Duration > bound {
		t.Errorf("incremental link under stale statistics took %s, bound %s", res.Duration, bound)
	}
	got, n := l.stateDigest(t, scope)
	want, wantN := l.aggregateDigest(t, scope, scope+"-g1")
	if got != want || n != wantN {
		t.Errorf("state after the link differs from the aggregate of g1")
	}
}
