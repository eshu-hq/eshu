// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// explainRebase plans statement (RebaseLinkSQL or a variant) for scopeID's
// g1 at the link's settings, inside a transaction it rolls back; prep runs
// first in the same transaction. With analyze the statement runs.
func (l *ledgerDB) explainRebase(t *testing.T, statement, scopeID string, analyze bool, prep ...string) string {
	t.Helper()
	tx := l.begin(t)
	defer func() { _ = tx.Rollback() }()
	for _, s := range append([]string{`SET LOCAL work_mem = '256MB'`, `SET LOCAL plan_cache_mode = force_custom_plan`}, prep...) {
		l.execTx(t, tx, s)
	}
	options := "FORMAT JSON"
	if analyze {
		options = "ANALYZE, BUFFERS, FORMAT JSON"
	}
	var plan string
	if err := tx.QueryRowContext(l.ctx, `EXPLAIN (`+options+`) `+statement,
		scopeID, scopeID+"-g1", linksfreshnessstore.DigestVersion, time.Now()).Scan(&plan); err != nil {
		t.Fatalf("explain rebase: %v", err)
	}
	return plan
}

// factRecordsFailure reports why a plan does not read fact_records exactly
// once, by a (scope_id, generation_id) index naming the scope and its g1.
func factRecordsFailure(nodes []planNode, scopeID string) string {
	accesses := accessesOf(nodes, "fact_records")
	if len(accesses) != 1 {
		return fmt.Sprintf("%d fact_records scans, want exactly 1", len(accesses))
	}
	a := accesses[0]
	if (a.indexName != "fact_records_scope_generation_idx" && a.indexName != "fact_records_scope_generation_keyset_idx") ||
		!strings.Contains(a.indexCond, "scope_id = '"+scopeID+"'") || !strings.Contains(a.indexCond, "generation_id = '"+scopeID+"-g1'") {
		return fmt.Sprintf("fact_records read by %s %q cond %q", a.nodeType, a.indexName, a.indexCond)
	}
	return ""
}

// TestRebaseLinkPlanShape is G3 for the rebase statement (#7127 PR-3e,
// arbiter ruling arb-7127-3d "FOR THE COORDINATOR TO FILE" item 1), with the
// P1 class of arb-7127-g8, which it inherits from the shared diff and state
// move. For a 600-key and a 45,000-key scope among 20 filler scopes of 20,000
// state rows, under the four planted statistics states: fact_records is read
// once by a (scope_id, generation_id) index for the activating generation;
// the state delete is a Tid Scan; no Nested Loop rescans its inner side; the
// state side is read by the primary key's scope_id prefix. One analyzed run
// fires no trigger. REDs: a scope predicate on the delete target breaks (a),
// and without the fact_records (scope, generation) indexes G3 fails.
func TestRebaseLinkPlanShape(t *testing.T) {
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
	sizes := map[string]int{"r600": 600, "r45k": 45000}
	for scopeID, keys := range sizes {
		l.seedRekeyScope(t, w, scopeID, keys, keys/10)
	}
	const anchor = "WHERE t.ctid = ANY"
	if !strings.Contains(linksfreshnessstore.RebaseLinkSQL, anchor) {
		t.Fatalf("RebaseLinkSQL has no %q; the planted RED cannot be built", anchor)
	}
	scopedDelete := strings.Replace(linksfreshnessstore.RebaseLinkSQL, anchor, "WHERE t.scope_id = $1 AND t.ctid = ANY", 1)

	for _, scopeID := range []string{"r600", "r45k"} {
		for _, state := range statsStates {
			l.plantStats(t, state, scopeID, sizes[scopeID])
			label := scopeID + "/" + state
			plan := l.explainRebase(t, linksfreshnessstore.RebaseLinkSQL, scopeID, false)
			failures, rows := planClassFailures(t, plan, state == "over")
			if state == "absent" && rows > 1 {
				t.Fatalf("%s: precondition not met, state side estimated at %.0f rows (want at most 1)", label, rows)
			}
			nodes, _ := planNodes(t, plan)
			if f := factRecordsFailure(nodes, scopeID); f != "" {
				failures = append(failures, "G3 "+f)
			}
			if len(failures) > 0 {
				t.Errorf("%s: RebaseLinkSQL breaks the plan class: %s", label, strings.Join(failures, "; "))
			} else {
				t.Logf("%s: plan class holds (state side estimated at %.0f rows)", label, rows)
			}
			if state != "absent" {
				continue
			}
			if red, _ := planClassFailures(t, l.explainRebase(t, scopedDelete, scopeID, false), false); !hasPrefix(red, "(a)") {
				t.Errorf("%s: the scoped delete passed (a); the gate cannot see it: %v", label, red)
			}
		}
		// One analyzed run at fresh statistics: no trigger fires, and the
		// fact_records access still holds with real row counts.
		l.plantStats(t, "fresh", scopeID, sizes[scopeID])
		nodes, triggers := planNodes(t, l.explainRebase(t, linksfreshnessstore.RebaseLinkSQL, scopeID, true))
		if f := factRecordsFailure(nodes, scopeID); f != "" || len(triggers) != 0 {
			t.Errorf("%s analyzed: %s; triggers %v", scopeID, f, triggers)
		}
		for _, relation := range []string{"fact_records", "changed_since_key_state"} {
			for _, a := range accessesOf(nodes, relation) {
				t.Logf("%s analyzed: %s by %s %q cond %q, %.0f rows, %.0f buffers",
					scopeID, relation, a.nodeType, a.indexName, a.indexCond, a.rows, a.buffers)
			}
		}
		// RED for G3: without the (scope, generation) indexes the access fails.
		red, _ := planNodes(t, l.explainRebase(t, linksfreshnessstore.RebaseLinkSQL, scopeID, false,
			`DROP INDEX fact_records_scope_generation_idx`, `DROP INDEX IF EXISTS fact_records_scope_generation_keyset_idx`))
		if factRecordsFailure(red, scopeID) == "" {
			t.Errorf("%s: G3 passed with the fact_records indexes dropped; the check cannot fail", scopeID)
		}
	}
}
