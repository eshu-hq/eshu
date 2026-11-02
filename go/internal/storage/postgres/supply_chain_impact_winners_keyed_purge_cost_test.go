// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// winnersPurgeCostEnv turns on the scope-delete cost measurement inside the
// scale proof: the number of findings in the scope that is deleted (#9805
// review F1). Zero or unset skips it.
const winnersPurgeCostEnv = "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_PURGE_FINDINGS"

func winnersPurgeCostFindings(t *testing.T) int {
	t.Helper()
	raw := winnersScaleKnob(winnersPurgeCostEnv)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		t.Fatalf("%s=%q: want a positive integer", winnersPurgeCostEnv, raw)
	}
	return n
}

// runWinnersPurgeCost writes one scope of n findings through the production
// writer, then runs EXPLAIN (ANALYZE, BUFFERS) of the retirement purge's scope
// DELETE inside a transaction it rolls back. The plan reports the BEFORE DELETE
// capture trigger's time and call count; the dirty-log row count read before
// the rollback is the number of identifiers the trigger appended. A second
// EXPLAIN of the trigger's own INSERT ... SELECT, with the same predicate,
// reports the buffers of the capture read.
func runWinnersPurgeCost(h *keyedHarness, corpus winnersScaleCorpus, n int) string {
	h.t.Helper()
	scope := "vuln-intel://9805-purge/s0"
	h.seedScope(scope, scope+"/g1")
	batch := corpus.findingsFrom(corpus.findings, n)
	h.write(scope, scope+"/g1", batch...)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)

	var out strings.Builder
	fmt.Fprintf(&out, "purge_cost scope_findings=%d host_load1=%s\n", n, hostLoad())
	tx, err := h.db.BeginTx(h.ctx, nil)
	if err != nil {
		h.t.Fatalf("begin purge cost: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	capture, err := tx.QueryContext(h.ctx, `EXPLAIN (ANALYZE, BUFFERS)
INSERT INTO supply_chain_impact_winners_dirty_log (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
SELECT DISTINCT 'fact', 'activation',
       COALESCE(fact.payload->>'package_id', ''),
       COALESCE(fact.payload->>'repository_id', ''),
       COALESCE(fact.payload->>'subject_digest', ''),
       COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
       COALESCE(fact.payload->>'advisory_id', '')
FROM fact_records AS fact
WHERE fact.scope_id = $1 AND fact.fact_kind = 'reducer_supply_chain_impact_finding' AND fact.is_tombstone = FALSE`, scope)
	if err != nil {
		h.t.Fatalf("explain capture read: %v", err)
	}
	out.WriteString("--- EXPLAIN (ANALYZE, BUFFERS) of the trigger's INSERT ... SELECT ---\n")
	for capture.Next() {
		var line string
		if err := capture.Scan(&line); err != nil {
			h.t.Fatalf("scan capture plan: %v", err)
		}
		out.WriteString(line + "\n")
	}
	if err := capture.Close(); err != nil {
		h.t.Fatalf("close capture plan: %v", err)
	}
	if _, err := tx.ExecContext(h.ctx, `DELETE FROM supply_chain_impact_winners_dirty_log`); err != nil {
		h.t.Fatalf("clear measured capture rows: %v", err)
	}
	del, err := tx.QueryContext(h.ctx, `EXPLAIN (ANALYZE, BUFFERS) DELETE FROM ingestion_scopes WHERE scope_id = $1`, scope)
	if err != nil {
		h.t.Fatalf("explain purge delete: %v", err)
	}
	out.WriteString("--- EXPLAIN (ANALYZE, BUFFERS) of the purge DELETE, trigger lines only ---\n")
	for del.Next() {
		var line string
		if err := del.Scan(&line); err != nil {
			h.t.Fatalf("scan delete plan: %v", err)
		}
		if strings.HasPrefix(line, "Trigger ") || strings.HasPrefix(line, "Execution Time") {
			out.WriteString(line + "\n")
		}
	}
	if err := del.Close(); err != nil {
		h.t.Fatalf("close delete plan: %v", err)
	}
	var appended int
	if err := tx.QueryRowContext(h.ctx, `SELECT count(*) FROM supply_chain_impact_winners_dirty_log WHERE kind = 'fact'`).Scan(&appended); err != nil {
		h.t.Fatalf("count appended rows: %v", err)
	}
	fmt.Fprintf(&out, "dirty-log fact rows appended by the delete trigger: %d\n", appended)
	return out.String()
}
