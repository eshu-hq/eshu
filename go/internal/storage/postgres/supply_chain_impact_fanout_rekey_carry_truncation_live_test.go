// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"testing"
)

// findingIdentities lists one scope's active finding rows as fact id, key,
// and package triples, for the no-duplicate diagnosis.
func (c *rekeyProof) findingIdentities(scope string) []string {
	c.t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT f.fact_id, f.stable_fact_key, COALESCE(f.payload->>'cve_id',''), COALESCE(f.payload->>'package_id','')
FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
  AND f.scope_id = $1 ORDER BY 1, 2, 3`, scope)
	if err != nil {
		c.t.Fatalf("read finding identities: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id, stable, key, pkg string
		if err := rows.Scan(&id, &stable, &key, &pkg); err != nil {
			c.t.Fatal(err)
		}
		out = append(out, id+" stable="+stable+" key="+key+" pkg="+pkg)
	}
	if err := rows.Err(); err != nil {
		c.t.Fatalf("iterate finding identities: %v", err)
	}
	return out
}

// findingStables returns the sorted stable keys of one scope's active
// finding rows, for the convergence comparisons.
func (c *rekeyProof) findingStables(scope string) []string {
	c.t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT DISTINCT f.stable_fact_key FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
  AND f.scope_id = $1 ORDER BY 1`, scope)
	if err != nil {
		c.t.Fatalf("read finding stables: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var stable string
		if err := rows.Scan(&stable); err != nil {
			c.t.Fatal(err)
		}
		out = append(out, stable)
	}
	if err := rows.Err(); err != nil {
		c.t.Fatalf("iterate finding stables: %v", err)
	}
	return out
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// TestRekeyTruncatedPassTombstonesNothingLive is the criterion-6 proof: a
// pass with a truncated alias closure (seeded evidence budget) must not
// retire old-group rows under the merge. It upserts per-record fallback keys
// (no member sets) with the truncation signal set, and the next full pass
// performs the re-key exactly once.
func TestRekeyTruncatedPassTombstonesNothingLive(t *testing.T) {
	c := newRekeyProof(t)
	c.holderBudget = 1
	c.seedCarryMergePair()
	c.drain()
	t.Logf("pre-merge findings: %v", c.findingKeys())

	c.rescanWithdrawDeadRecord()
	c.rerun(rekeyCommitter)
	c.handled = nil
	truncated := c.drainCapturing()
	t.Logf("post-merge findings: %v", c.findingKeys())
	t.Logf("post-merge handled: %v", c.handled)
	if c.handledCount(rekeyHolder) == 0 {
		t.Fatalf("holder never reopened after the merge (handled %v)", c.handled)
	}
	// The truncated holder pass must not tombstone: the dead-keyed row it
	// held before the merge is still active.
	if got := c.keysByScope(rekeyHolder); len(got) == 0 {
		t.Fatalf("holder findings = %v, want the pre-merge rows intact (a truncated pass retracts nothing)", got)
	} else {
		seen := map[string]bool{}
		for _, key := range got {
			seen[key] = true
		}
		if !seen[rekeyDead] {
			t.Fatalf("holder keys = %v, want the dead id [%s] still active after the truncated pass", got, rekeyDead)
		}
	}
	holderPass := truncated[rekeyHolder+"@"+fanOutGen(rekeyHolder, "g1")]
	if got := holderPass.SubSignals["evidence_partial"]; got != 1 {
		t.Fatalf("truncated holder evidence_partial = %v, want 1 (the budget truncation is signalled)", got)
	}
	if got := holderPass.SubSignals["findings_retracted"]; got != 0 {
		t.Fatalf("truncated holder findings_retracted = %v, want 0 (a partial pass proves no removal)", got)
	}
	// Fallback keys: with grouping disabled the truncated pass emits
	// per-record rows with no advisory-group member sets.
	rows, err := c.db.QueryContext(c.ctx, `
SELECT DISTINCT COALESCE(f.payload #>> '{provenance,advisory_group_members}', '')
FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
  AND f.scope_id = $1`, rekeyHolder)
	if err != nil {
		t.Fatalf("read holder member sets: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var members string
		if err := rows.Scan(&members); err != nil {
			t.Fatal(err)
		}
		if members != "" && members != "[]" && members != "null" {
			t.Fatalf("truncated holder member set = %q, want empty (per-record fallback keys)", members)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate holder member sets: %v", err)
	}
	// Fallback keys: the truncated pass derives per-record keys with no
	// member sets, since grouping stays disabled on partial alias graphs.
	for _, row := range c.findingKeys() {
		t.Logf("row: %s", row)
	}
	for _, row := range c.findingIdentities(rekeyHolder) {
		t.Logf("holder row: %s", row)
	}

	// The next full pass re-keys exactly once: the dead rows retire, the
	// survivor rows carry the merged member set, and the holder converges on
	// exactly the committer's finding set — no missing rows, no duplicates.
	c.holderHandler = nil
	c.holderBudget = 0
	c.rerun(rekeyHolder)
	c.handled = nil
	full := c.drainCapturing()
	t.Logf("post-full-pass findings: %v", c.findingKeys())
	if got := c.keysByScope(rekeyHolder); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("holder keys = %v, want the survivor group [%s] after the full pass", got, rekeySurvivor)
	}
	for _, row := range c.findingIdentities(rekeyHolder) {
		t.Logf("full holder row: %s", row)
	}
	if got, want := c.findingStables(rekeyHolder), c.findingStables(rekeyCommitter); !equalStrings(got, want) {
		t.Fatalf("holder stables = %v, committer stables = %v, want the same converged set re-keyed once", got, want)
	}
	fullPass := full[rekeyHolder+"@"+fanOutGen(rekeyHolder, "g1")]
	if got := fullPass.SubSignals["evidence_partial"]; got != 0 {
		t.Fatalf("full holder evidence_partial = %v, want 0 (the closure is complete now)", got)
	}
	if got := c.pendingWorkItems(rekeyHolder); got != 0 {
		t.Fatalf("holder pending work items = %d, want 0 (no dead-letter on convergence)", got)
	}
}
