// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// chainDeltaProbe is the ev access of the shim's chain read (chain_read_v3.sql):
// the links on the path from $3 back to $2 and their delta rows. The PR-3c
// read statement builds on this access; gate G4 pins its plan now, because
// migration 134 freezes the primary key's column order.
//
// ev is the LATERAL form of #7127 ruling 8.4. The plain join form of
// chain_read_v3.sql fails this gate: with statistics taken at 13k rows and
// the table grown to 2M, its custom plan is a Bitmap Heap Scan whose index
// condition names scope_id only, reading every retained delta of the scope
// (evidence file, G4). The LATERAL subquery with OFFSET 0 is planned per link
// as a parameterized index scan on the full (scope, generation, prior)
// prefix.
const chainDeltaProbe = `
WITH RECURSIVE chain AS (
  SELECT l.generation_id, l.prior_generation_id, 1 AS depth, ARRAY[l.generation_id] AS path
  FROM changed_since_links l WHERE l.scope_id = $1 AND l.generation_id = $3
  UNION ALL
  SELECT l.generation_id, l.prior_generation_id, c.depth + 1, c.path || l.generation_id
  FROM chain c JOIN changed_since_links l ON l.scope_id = $1 AND l.generation_id = c.prior_generation_id
  WHERE c.prior_generation_id <> $2 AND l.generation_id <> ALL(c.path) AND c.depth < 256
),
reach AS (SELECT path FROM chain WHERE prior_generation_id = $2 ORDER BY depth LIMIT 1),
links AS (
  SELECT u.gen AS generation_id, u.ord AS depth, COALESCE(lead(u.gen) OVER (ORDER BY u.ord), $2) AS prior_generation_id
  FROM reach, unnest(reach.path) WITH ORDINALITY AS u(gen, ord)
),
ev AS (
  SELECT d.fact_category AS cat, d.stable_fact_key, l.depth, d.prior_state, d.current_state
  FROM links l CROSS JOIN LATERAL (
    SELECT dd.fact_category, dd.stable_fact_key, dd.prior_state, dd.current_state
    FROM changed_since_link_deltas dd
    WHERE dd.scope_id = $1 AND dd.generation_id = l.generation_id AND dd.prior_generation_id = l.prior_generation_id
    OFFSET 0
  ) AS d
)
SELECT cat, stable_fact_key, depth FROM ev`

// seedLinkDeltas writes links k = fromLink..toLink (a chain gK -> gK-1) of
// scopes 0..scopes-1, each with rowsPerLink delta rows.
func (l *ledgerDB) seedLinkDeltas(t *testing.T, scopes, fromLink, toLink, rowsPerLink int) {
	t.Helper()
	l.exec(t, `
INSERT INTO changed_since_link_deltas
    (scope_id, generation_id, prior_generation_id, fact_category, classification, stable_fact_key,
     prior_fact_kind, current_fact_kind, prior_state, current_state, current_tombstoned)
SELECT 's' || lpad(s::text, 4, '0'), 'g' || k, 'g' || (k - 1), 'content_entities', 'updated',
       'ent:' || lpad(r::text, 6, '0'), 'content_entity', 'content_entity',
       sha256(('p' || s || k || r)::bytea), sha256(('c' || s || k || r)::bytea), FALSE
FROM generate_series(0, $1 - 1) AS s, generate_series($2::int, $3::int) AS k, generate_series(1, $4) AS r`,
		scopes, fromLink, toLink, rowsPerLink)
}

func explainPrepared(t *testing.T, ctx context.Context, conn *sql.Conn, mode, statement, args string) string {
	t.Helper()
	for _, s := range []string{
		`SET plan_cache_mode = ` + mode,
		`PREPARE plan_probe(text, text, text) AS ` + statement,
	} {
		if _, err := conn.ExecContext(ctx, s); err != nil {
			t.Fatalf("%s: %v", firstLine(s), err)
		}
	}
	defer func() { _, _ = conn.ExecContext(ctx, `DEALLOCATE plan_probe`) }()
	var plan string
	if err := conn.QueryRowContext(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE plan_probe(`+args+`)`).Scan(&plan); err != nil {
		t.Fatalf("explain: %v", err)
	}
	return plan
}

// TestLinkDeltaChainReadUsesPrimaryKey is the first plan-shape test of gate G4
// (#7127 ruling 8.4): 2,000,000 delta rows over 500 scopes and 5,000 links,
// the probed scope holding 0.2% of the rows over 4 links, in both plan-cache
// modes, with stale and with fresh statistics, and RED without the key.
func TestLinkDeltaChainReadUsesPrimaryKey(t *testing.T) {
	l := openLedgerDB(t)
	l.exec(t, `ALTER TABLE changed_since_link_deltas SET (autovacuum_enabled = false)`)
	l.exec(t, `
INSERT INTO changed_since_links (scope_id, generation_id, prior_generation_id, link_kind, digest_version,
    delta_rows, files_keys, content_entities_keys, facts_keys, computed_at)
SELECT 's' || lpad(s::text, 4, '0'), 'g' || k, 'g' || (k - 1), 'incremental', 1, 400, 0, 400, 0, now()
FROM generate_series(0, 499) AS s, generate_series(1, 10) AS k`)
	l.exec(t, `ANALYZE changed_since_links`)
	// Stale statistics: ANALYZE at about 13k rows, then grow to 2M.
	l.seedLinkDeltas(t, 33, 1, 1, 400)
	l.exec(t, `ANALYZE changed_since_link_deltas`)
	l.exec(t, `DELETE FROM changed_since_link_deltas`)
	l.seedLinkDeltas(t, 500, 1, 10, 400)
	if n := l.queryInt(t, `SELECT count(*) FROM changed_since_link_deltas`); n != 2000000 {
		t.Fatalf("fixture has %d delta rows, want 2,000,000", n)
	}
	conn, err := l.raw.Conn(l.ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	columns := []string{"scope_id", "generation_id", "prior_generation_id"}
	const args = `'s0007', 'g6', 'g10'`
	check := func(label string) {
		for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
			nodes, _ := planNodes(t, explainPrepared(t, l.ctx, conn, mode, chainDeltaProbe, args))
			if failures := indexAccessFailures(nodes, "changed_since_link_deltas", "changed_since_link_deltas_pkey", columns, 10); len(failures) > 0 {
				t.Fatalf("%s, %s: %s", label, mode, strings.Join(failures, "; "))
			}
			rows := accessesOf(nodes, "changed_since_link_deltas")[0].rows
			if rows != 1600 {
				t.Fatalf("%s, %s: probe read %.0f delta rows, want 1600 (4 links)", label, mode, rows)
			}
		}
	}
	check("stale statistics")

	// Guard: the plain join form of ev from the shim's chain_read_v3.sql
	// fails the same assertions with stale statistics (a bitmap scan whose
	// index condition names scope_id only), which is why PR-3c must use the
	// LATERAL form above. If this ever passes, re-evaluate the LATERAL rule.
	plainJoin := strings.Replace(chainDeltaProbe, `FROM links l CROSS JOIN LATERAL (
    SELECT dd.fact_category, dd.stable_fact_key, dd.prior_state, dd.current_state
    FROM changed_since_link_deltas dd
    WHERE dd.scope_id = $1 AND dd.generation_id = l.generation_id AND dd.prior_generation_id = l.prior_generation_id
    OFFSET 0
  ) AS d`, `FROM links l JOIN changed_since_link_deltas d
    ON d.scope_id = $1 AND d.generation_id = l.generation_id AND d.prior_generation_id = l.prior_generation_id`, 1)
	if plainJoin == chainDeltaProbe {
		t.Fatal("plain-join guard: LATERAL anchor not found in chainDeltaProbe")
	}
	plainNodes, _ := planNodes(t, explainPrepared(t, l.ctx, conn, "force_custom_plan", plainJoin, args))
	if failures := indexAccessFailures(plainNodes, "changed_since_link_deltas", "changed_since_link_deltas_pkey", columns, 10); len(failures) == 0 {
		t.Fatalf("plain-join guard: the join form passed with stale statistics; re-evaluate the LATERAL rule for PR-3c")
	} else {
		t.Logf("plain-join guard (expected RED): %s", strings.Join(failures, "; "))
	}

	// The runner's candidate read (#7127 ruling 8.10) at this fixture size:
	// one journal row per link (5,000) and one cursor per scope (500), a
	// fifth of them backing off. It reads only the journal and the cursor.
	l.exec(t, `
INSERT INTO changed_since_activations (scope_id, generation_id, prior_generation_id, source, activated_at)
SELECT 's' || lpad(s::text, 4, '0'), 'g' || k, NULL, 'sweeper', now()
FROM generate_series(0, 499) AS s, generate_series(1, 10) AS k ORDER BY k, s`)
	l.exec(t, `
INSERT INTO changed_since_scope_cursor (scope_id, state_activation_seq, digest_version, updated_at, next_attempt_at)
SELECT 's' || lpad(s::text, 4, '0'), (s % 7) * 500, 1, now(), CASE WHEN s % 5 = 0 THEN now() + interval '1 hour' END
FROM generate_series(0, 499) AS s`)
	l.exec(t, `ANALYZE changed_since_activations`)
	l.exec(t, `ANALYZE changed_since_scope_cursor`)
	var candidatePlan string
	if err := l.raw.QueryRowContext(l.ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+linksfreshnessstore.BacklogScopesQueryForTest,
		32, time.Now()).Scan(&candidatePlan); err != nil {
		t.Fatalf("explain candidates: %v", err)
	}
	candidateNodes, _ := planNodes(t, candidatePlan)
	for _, n := range candidateNodes {
		if rel := n.str("Relation Name"); rel != "" && rel != "changed_since_activations" && rel != "changed_since_scope_cursor" {
			t.Fatalf("candidate read touches %s", rel)
		}
	}
	var doc []map[string]any
	_ = json.Unmarshal([]byte(candidatePlan), &doc)
	t.Logf("candidate read over 5,000 journal rows and 500 cursors: %.2f ms execution", doc[0]["Execution Time"])
	if ms, _ := doc[0]["Execution Time"].(float64); ms > 1000 {
		t.Fatalf("candidate read took %.0f ms at the 8.4 fixture size", ms)
	}
	l.exec(t, `ANALYZE changed_since_link_deltas`)
	check("fresh statistics")

	// RED: without the primary key the same assertions must fail.
	if _, err := conn.ExecContext(l.ctx, `BEGIN`); err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _, _ = conn.ExecContext(l.ctx, `ROLLBACK`) }()
	if _, err := conn.ExecContext(l.ctx, `ALTER TABLE changed_since_link_deltas DROP CONSTRAINT changed_since_link_deltas_pkey`); err != nil {
		t.Fatalf("drop key: %v", err)
	}
	nodes, _ := planNodes(t, explainPrepared(t, l.ctx, conn, "force_generic_plan", chainDeltaProbe, args))
	if failures := indexAccessFailures(nodes, "changed_since_link_deltas", "changed_since_link_deltas_pkey", columns, 10); len(failures) == 0 {
		t.Fatalf("plan-shape assertions passed without the primary key; the gate cannot see a lost index")
	}
}

// TestLinkStatementPlanShape covers gate G3 (the incremental statement reads
// fact_records once, on fact_records_scope_generation_idx, for the
// activating generation, fires no trigger, with and without ANALYZE of the
// loaded generation) and the state-table half of gate G4: 3.8M state rows
// over 800 scopes, and a 600-key and a 45,000-key scope read their state by
// the primary key's scope_id prefix, never by a sequential scan.
func TestLinkStatementPlanShape(t *testing.T) {
	l := openLedgerDB(t)
	l.exec(t, `ALTER TABLE fact_records SET (autovacuum_enabled = false)`)
	l.exec(t, `ALTER TABLE changed_since_key_state SET (autovacuum_enabled = false)`)
	for _, scope := range []struct {
		id   string
		keys int
	}{{"small", 600}, {"mid", 45000}} {
		l.seedScope(t, scope.id)
		l.seedGeneration(t, scope.id, scope.id+"-g0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
		l.seedGeneration(t, scope.id, scope.id+"-g1", false, "active", fixtureEpoch.Add(time.Hour), time.Time{})
		l.seedBulkGeneration(t, scope.id, scope.id+"-g0", scope.keys, 0)
		l.seedBulkGeneration(t, scope.id, scope.id+"-g1", scope.keys, 1)
		l.journal(t, scope.id, scope.id+"-g0", "")
		l.journal(t, scope.id, scope.id+"-g1", scope.id+"-g0")
		mustLink(t, linksfreshnessstore.NewLinkWriter(l.store), l, scope.id)
	}
	for i := range 20 {
		noise := "noise-" + string(rune('a'+i))
		l.seedScope(t, noise)
		l.seedGeneration(t, noise, noise+"-g0", false, "active", fixtureEpoch, time.Time{})
		l.seedBulkGeneration(t, noise, noise+"-g0", 10000, 0)
	}
	l.exec(t, `
INSERT INTO changed_since_key_state (scope_id, fact_category, stable_fact_key, fact_kind, state, owner_uri)
SELECT 'fill' || lpad(s::text, 4, '0'), 'content_entities', 'ent:' || lpad(k::text, 6, '0'), 'content_entity',
       sha256(('f' || s || k)::bytea), 'f' || (k / 50) || '.go'
FROM generate_series(0, 797) AS s, generate_series(1, 4760) AS k`)
	l.exec(t, `ANALYZE changed_since_key_state`)
	if n := l.queryInt(t, `SELECT count(*) FROM changed_since_key_state`); n < 3800000 {
		t.Fatalf("state fixture has %d rows, want at least 3,800,000", n)
	}

	explainLink := func(scopeID string) (nodes []planNode, triggers []any) {
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
		if err := tx.QueryRowContext(l.ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+linksfreshnessstore.IncrementalLinkSQL,
			scopeID, scopeID+"-g1", scopeID+"-g0", linksfreshnessstore.DigestVersion, time.Now()).Scan(&plan); err != nil {
			t.Fatalf("explain incremental: %v", err)
		}
		return planNodes(t, plan)
	}
	checkG3 := func(label, scopeID string, nodes []planNode, triggers []any) {
		t.Helper()
		accesses := accessesOf(nodes, "fact_records")
		if len(accesses) != 1 {
			t.Fatalf("G3 %s %s: %d fact_records scans, want exactly 1", label, scopeID, len(accesses))
		}
		a := accesses[0]
		// Ruling 8.6 names fact_records_scope_generation_idx; the shim's
		// fixture had only migration 003's indexes. On the full bootstrap the
		// planner may also pick fact_records_scope_generation_keyset_idx
		// (scope_id, generation_id, observed_at, fact_id, migration 099).
		// Both are (scope_id, generation_id, ...) prefix range scans and the
		// planner picks between them by cost, so either passes, but only
		// with exactly one fact_records scan whose index condition binds both
		// scope_id and the activating generation. Any other index, an index
		// condition on scope_id alone, or a sequential scan fails.
		scopeGenerationIndex := a.indexName == "fact_records_scope_generation_idx" ||
			a.indexName == "fact_records_scope_generation_keyset_idx"
		if !scopeGenerationIndex || !strings.Contains(a.indexCond, "scope_id = '"+scopeID+"'") ||
			!strings.Contains(a.indexCond, "generation_id = '"+scopeID+"-g1'") {
			t.Fatalf("G3 %s %s: fact_records read by %s %q cond %q, want a (scope_id, generation_id) index naming %s-g1",
				label, scopeID, a.nodeType, a.indexName, a.indexCond, scopeID)
		}
		t.Logf("G3 %s %s: one fact_records %s on %s, cond %s", label, scopeID, a.nodeType, a.indexName, a.indexCond)
		if len(triggers) != 0 {
			t.Fatalf("G3 %s %s: triggers fired: %v", label, scopeID, triggers)
		}
	}
	for _, label := range []string{"no ANALYZE of fact_records", "after ANALYZE"} {
		if label == "after ANALYZE" {
			l.exec(t, `ANALYZE fact_records`)
		}
		for _, scopeID := range []string{"small", "mid"} {
			nodes, triggers := explainLink(scopeID)
			checkG3(label, scopeID, nodes, triggers)
			if failures := indexAccessFailures(nodes, "changed_since_key_state", "changed_since_key_state_pkey",
				[]string{"scope_id"}, 0); len(failures) > 0 {
				t.Fatalf("G4 state %s %s: %s", label, scopeID, strings.Join(failures, "; "))
			}
		}
	}

	// The 600-key link, through the writer, at most 100 ms.
	result := mustLink(t, linksfreshnessstore.NewLinkWriter(l.store), l, "small")
	if result.Kind != linksfreshnessstore.LinkKindIncremental {
		t.Fatalf("small link = %+v", result)
	}
	t.Logf("600-key incremental link: %s (%d delta rows)", result.Duration, result.DeltaRows)
	if result.Duration > 100*time.Millisecond {
		t.Errorf("600-key link took %s, gate G4 allows 100ms (host load may explain it; see the evidence file)", result.Duration)
	}
}
