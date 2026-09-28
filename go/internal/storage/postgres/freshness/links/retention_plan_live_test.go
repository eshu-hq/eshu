// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// seedRetentionPlanFixture writes gate G4's scale for retention: scopes
// 0..scopes-1, each with generations g0..g10 (globally unique ids, as in
// production), ten links gK -> gK-1 with rowsPerLink delta rows and two bucket
// rows each, and one activation per generation. It writes no fact_records.
func (l *ledgerDB) seedRetentionPlanFixture(t *testing.T, scopes, rowsPerLink int) {
	t.Helper()
	l.exec(t, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
    observed_at, ingested_at, status)
SELECT 'ps' || lpad(s::text, 4, '0'), 'repository', 'git', 'ps' || s, 'git', 'ps' || s, now(), now(), 'active'
FROM generate_series(0, $1 - 1) AS s
ON CONFLICT DO NOTHING`, scopes)
	l.exec(t, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'ps' || lpad(s::text, 4, '0') || '-g' || k, 'ps' || lpad(s::text, 4, '0'), 'snapshot', now(), now(), 'superseded'
FROM generate_series(0, $1 - 1) AS s, generate_series(0, 10) AS k
ON CONFLICT DO NOTHING`, scopes)
	l.exec(t, `
INSERT INTO changed_since_links (scope_id, generation_id, prior_generation_id, link_kind, digest_version,
    delta_rows, files_keys, content_entities_keys, facts_keys, computed_at)
SELECT 'ps' || lpad(s::text, 4, '0'), 'ps' || lpad(s::text, 4, '0') || '-g' || k,
       'ps' || lpad(s::text, 4, '0') || '-g' || (k - 1), 'incremental', 1, $2, 0, $2, 0, now()
FROM generate_series(0, $1 - 1) AS s, generate_series(1, 10) AS k
ON CONFLICT DO NOTHING`, scopes, rowsPerLink)
	l.exec(t, `
INSERT INTO changed_since_link_deltas
    (scope_id, generation_id, prior_generation_id, fact_category, classification, stable_fact_key,
     prior_fact_kind, current_fact_kind, prior_state, current_state, current_tombstoned)
SELECT link.scope_id, link.generation_id, link.prior_generation_id, 'content_entities', 'updated',
       'ent:' || lpad(r::text, 6, '0'), 'content_entity', 'content_entity',
       sha256(('p' || link.generation_id || r)::bytea), sha256(('c' || link.generation_id || r)::bytea), FALSE
FROM changed_since_links AS link, generate_series(1, $1) AS r
ON CONFLICT DO NOTHING`, rowsPerLink)
	l.exec(t, `
INSERT INTO changed_since_link_bucket_counts (scope_id, generation_id, prior_generation_id, fact_category,
    classification, key_count)
SELECT scope_id, generation_id, prior_generation_id, 'content_entities', c, delta_rows
FROM changed_since_links, unnest(ARRAY['updated', 'added']) AS c
ON CONFLICT DO NOTHING`)
	l.exec(t, `
INSERT INTO changed_since_activations (scope_id, generation_id, prior_generation_id, source, activated_at)
SELECT scope_id, generation_id, NULL, 'sweeper', now() FROM scope_generations
ORDER BY generation_id
ON CONFLICT DO NOTHING`)
}

// disableLedgerAutovacuum keeps the planner statistics where the test put
// them.
func (l *ledgerDB) disableLedgerAutovacuum(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"changed_since_links", "changed_since_link_deltas", "changed_since_link_bucket_counts",
		"changed_since_activations", "scope_generations", "ingestion_scopes",
	} {
		l.exec(t, `ALTER TABLE `+table+` SET (autovacuum_enabled = false)`)
	}
}

// explainThroughDriver runs EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) of a
// retention statement with the store's own argument binding (two Go []string
// bound as text[] by the driver), in plan-cache mode (empty keeps the
// server's default, auto), inside a transaction it rolls back.
func explainThroughDriver(t *testing.T, ctx context.Context, conn *sql.Conn, mode, statement string, scopes, gens []string) []planNode {
	t.Helper()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if mode != "" {
		if _, err := tx.ExecContext(ctx, `SET LOCAL plan_cache_mode = `+mode); err != nil {
			t.Fatalf("set plan_cache_mode: %v", err)
		}
	}
	var plan string
	if err := tx.QueryRowContext(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+statement, scopes, gens).Scan(&plan); err != nil {
		t.Fatalf("explain: %v", err)
	}
	nodes, _ := planNodes(t, plan)
	var doc []map[string]any
	_ = json.Unmarshal([]byte(plan), &doc)
	t.Logf("mode %q: %.1f ms", mode, doc[0]["Execution Time"])
	return nodes
}

// badInnerChild lists join nodes whose inner child reads a relation or CTE
// per outer row (arbiter ruling arb-7127-g8 P1's class assertion): a Nested
// Loop, Semi Join or Anti Join with a CTE Scan, Seq Scan, Function Scan or
// Materialize inside.
func badInnerChild(nodes []planNode) []string {
	var failures []string
	for _, n := range nodes {
		joinType := n.str("Join Type")
		if n.str("Node Type") != "Nested Loop" && joinType != "Semi" && joinType != "Anti" {
			continue
		}
		children, _ := n["Plans"].([]any)
		for _, c := range children {
			child, ok := c.(map[string]any)
			if !ok || planNode(child).str("Parent Relationship") != "Inner" {
				continue
			}
			switch inner := planNode(child).str("Node Type"); inner {
			case "CTE Scan", "Seq Scan", "Function Scan", "Materialize":
				failures = append(failures, fmt.Sprintf("%s (%s join) has a %s as its inner child", n.str("Node Type"), joinType, inner))
			}
		}
	}
	return failures
}

// retentionPlanFailures lists why a retention plan is not index-driven: link
// deltas (prune only) and bucket counts are read through their primary key on
// the full (scope, generation, prior) prefix, at most 10 buffers per row, or
// by a Tid Scan fed by such a read; no Seq Scan touches the delta table in
// either statement; changed_since_links and changed_since_activations are
// each read once; and no join reads a relation per outer row.
func retentionPlanFailures(all []planNode, prune bool) []string {
	columns := []string{"scope_id", "generation_id", "prior_generation_id"}
	var nodes []planNode
	for _, n := range all {
		if n.str("Node Type") != "Tid Scan" {
			nodes = append(nodes, n)
		}
	}
	failures := indexAccessFailures(nodes, "changed_since_link_bucket_counts", "changed_since_link_bucket_counts_pkey", columns, 10)
	if prune {
		failures = append(failures, indexAccessFailures(nodes, "changed_since_link_deltas", "changed_since_link_deltas_pkey", columns, 10)...)
	}
	for _, n := range all {
		switch n.str("Relation Name") {
		case "changed_since_link_deltas":
			if n.str("Node Type") == "Seq Scan" {
				failures = append(failures, "Seq Scan on changed_since_link_deltas")
			}
		case "changed_since_links", "changed_since_activations":
			if n.str("Node Type") != "ModifyTable" && n.num("Actual Loops") > 1 {
				failures = append(failures, fmt.Sprintf("%s on %s ran %.0f times", n.str("Node Type"), n.str("Relation Name"), n.num("Actual Loops")))
			}
		}
	}
	return append(failures, badInnerChild(all)...)
}

// retentionPlanBatch is 100 candidates, one per fifth scope: 200 links and
// 80,000 delta rows at 400 per link.
func retentionPlanBatch() (scopes, gens []string) {
	for s := 0; s < 500; s += 5 {
		scopes = append(scopes, fmt.Sprintf("ps%04d", s))
		gens = append(gens, fmt.Sprintf("ps%04d-g5", s))
	}
	return scopes, gens
}

// checkRetentionPlans asserts the plan class of both retention statements in
// all three plan-cache modes, and that the prune reads the batch's 80,000
// delta rows.
func checkRetentionPlans(t *testing.T, l *ledgerDB, label string) {
	t.Helper()
	conn, err := l.raw.Conn(l.ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	scopes, gens := retentionPlanBatch()
	for _, mode := range []string{"", "force_custom_plan", "force_generic_plan"} {
		for _, stmt := range []struct {
			name  string
			sql   string
			prune bool
		}{
			{"count", linksfreshnessstore.RetentionRowCountsQueryForTest, false},
			{"prune", linksfreshnessstore.RetentionPruneQueryForTest, true},
		} {
			nodes := explainThroughDriver(t, l.ctx, conn, mode, stmt.sql, scopes, gens)
			if failures := retentionPlanFailures(nodes, stmt.prune); len(failures) > 0 {
				t.Fatalf("%s, %s, mode %q: %s", label, stmt.name, mode, strings.Join(failures, "; "))
			}
			if !stmt.prune {
				continue
			}
			var deltaRows float64
			for _, a := range accessesOf(nodes, "changed_since_link_deltas") {
				if a.nodeType != "Tid Scan" {
					deltaRows += a.rows
				}
			}
			if deltaRows != 80000 {
				t.Fatalf("%s, mode %q: prune read %.0f delta rows, want 80,000 (200 links)", label, mode, deltaRows)
			}
		}
	}
}

// TestRetentionStatementPlanShape is P6 of arbiter ruling arb-7127-3d, the
// ruling 8.4 sweep for the retention statements: 2,000,000 delta rows over 500
// scopes and 5,000 links, a batch of 100 candidates, through the driver's own
// binding, in auto, custom and generic plan-cache modes, with four statistics
// states: never analyzed, fresh, analyzed empty then loaded, and stale
// (analyzed at 5 scopes and grown). RED: a planted per-scope LATERAL read of
// the links, and the delta table without its primary key.
func TestRetentionStatementPlanShape(t *testing.T) {
	t.Run("never analyzed, then fresh", func(t *testing.T) {
		l := openLedgerDB(t)
		l.disableLedgerAutovacuum(t)
		l.seedRetentionPlanFixture(t, 500, 400)
		if n := l.queryInt(t, `SELECT count(*) FROM changed_since_link_deltas`); n != 2000000 {
			t.Fatalf("fixture has %d delta rows, want 2,000,000", n)
		}
		checkRetentionPlans(t, l, "never analyzed")
		l.exec(t, `ANALYZE`)
		checkRetentionPlans(t, l, "fresh")

		// RED: without the delta primary key the prune must fail.
		conn, err := l.raw.Conn(l.ctx)
		if err != nil {
			t.Fatalf("conn: %v", err)
		}
		defer func() { _ = conn.Close() }()
		tx, err := conn.BeginTx(l.ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(l.ctx, `ALTER TABLE changed_since_link_deltas DROP CONSTRAINT changed_since_link_deltas_pkey`); err != nil {
			t.Fatalf("drop key: %v", err)
		}
		scopes, gens := retentionPlanBatch()
		var plan string
		if err := tx.QueryRowContext(l.ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+linksfreshnessstore.RetentionPruneQueryForTest,
			scopes, gens).Scan(&plan); err != nil {
			t.Fatalf("explain without key: %v", err)
		}
		nodes, _ := planNodes(t, plan)
		if failures := retentionPlanFailures(nodes, true); len(failures) == 0 {
			t.Fatal("plan-shape assertions passed without the delta primary key; the gate cannot see a lost index")
		}
	})

	t.Run("analyzed empty, then loaded", func(t *testing.T) {
		l := openLedgerDB(t)
		l.disableLedgerAutovacuum(t)
		l.exec(t, `ANALYZE`)
		l.seedRetentionPlanFixture(t, 500, 400)
		checkRetentionPlans(t, l, "analyzed empty")
	})

	t.Run("stale", func(t *testing.T) {
		l := openLedgerDB(t)
		l.disableLedgerAutovacuum(t)
		l.seedRetentionPlanFixture(t, 5, 400)
		l.exec(t, `ANALYZE`)
		l.seedRetentionPlanFixture(t, 500, 400)
		checkRetentionPlans(t, l, "stale")

		// RED: the per-scope LATERAL read of the links, which stale statistics
		// plan as one sequential scan per scope.
		planted := strings.Replace(linksfreshnessstore.RetentionPruneQueryForTest, `    FROM changed_since_links AS link
    WHERE link.scope_id = ANY ($1::text[])`, `    FROM (SELECT DISTINCT unnest($1::text[]) AS scope_id) AS pruned
    CROSS JOIN LATERAL (SELECT l.ctid, l.* FROM changed_since_links AS l WHERE l.scope_id = pruned.scope_id OFFSET 0) AS link
    WHERE TRUE`, 1)
		if planted == linksfreshnessstore.RetentionPruneQueryForTest {
			t.Fatal("planted LATERAL: anchor not found in the prune statement")
		}
		conn, err := l.raw.Conn(l.ctx)
		if err != nil {
			t.Fatalf("conn: %v", err)
		}
		defer func() { _ = conn.Close() }()
		scopes, gens := retentionPlanBatch()
		if failures := retentionPlanFailures(explainThroughDriver(t, l.ctx, conn, "force_custom_plan", planted, scopes, gens), true); len(failures) == 0 {
			t.Fatal("planted per-scope LATERAL passed with stale statistics; the gate cannot see a per-scope scan")
		} else {
			t.Logf("planted per-scope LATERAL (expected RED): %s", strings.Join(failures, "; "))
		}
	})
}
