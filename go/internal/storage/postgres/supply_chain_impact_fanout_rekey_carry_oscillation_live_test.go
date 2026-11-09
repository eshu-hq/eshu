// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

const (
	carryOscA = "CVE-2026-0331"
	carryOscB = "CVE-2026-0332"
)

// seedOscillationPair seeds two lone CVE groups with one package each, in a
// single scope with no operator suppressions: pure identity oscillation.
func (c *rekeyProof) seedOscillationPair() {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyCommitter, "g1")
	c.upsert(
		c.envelope(rekeyCommitter, "g1", "fact:10408:oa", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": carryOscA, "cve_id": carryOscA, "source": "ghsa", "cvss_score": 7.5, "aliases": []string{carryOscA},
		}),
		c.envelope(rekeyCommitter, "g1", "fact:10408:ob", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": carryOscB, "cve_id": carryOscB, "source": "nvd", "cvss_score": 5.0, "aliases": []string{carryOscB},
		}),
		c.affectedPackage(rekeyCommitter, "g1", "fact:10408:opkg", carryOscA, rekeyPackageA),
		c.affectedPackage(rekeyCommitter, "g1", "fact:10408:odpkg", carryOscB, rekeyPackageB),
		c.consumption(rekeyCommitter, "g1", "fact:10408:ocons", rekeyPackageA, "repository:r_10408_c"),
		c.consumption(rekeyCommitter, "g1", "fact:10408:odcons", rekeyPackageB, "repository:r_10408_c"),
	)
	fanOutSeedItem(c.t, c.ctx, c.db, rekeyCommitter, "g1")
}

// rescanOscillation copies the scope to the next generation with the A
// record's alias list replaced, modelling one alias add or removal.
func (c *rekeyProof) rescanOscillation(from, to string, aliases []string) {
	c.t.Helper()
	c.copyGeneration(rekeyCommitter, from, to)
	encoded, err := json.Marshal(aliases)
	if err != nil {
		c.t.Fatalf("marshal aliases: %v", err)
	}
	// Chained copies accumulate fact-id suffixes (oa:g2:g3), so match the
	// lineage by prefix: one A record exists per generation.
	res, err := c.db.ExecContext(c.ctx, `
UPDATE fact_records SET payload = jsonb_set(payload, '{aliases}', $3::jsonb)
WHERE scope_id = $1 AND generation_id = $2 AND fact_id LIKE 'fact:10408:oa:%'`, rekeyCommitter, fanOutGen(rekeyCommitter, to), string(encoded))
	if err != nil {
		c.t.Fatalf("rewrite aliases: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		c.t.Fatalf("rewrite aliases affected %d rows, want 1 (the A record lineage)", n)
	}
	// copyGeneration already seeded the new generation's queue item.
}

// workItemStates lists one scope's reducer work items for the dead-letter
// diagnosis.
func (c *rekeyProof) workItemStates(scope string) []string {
	c.t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT generation_id, status, attempt_count FROM fact_work_items
WHERE scope_id = $1 AND domain = 'supply_chain_impact' ORDER BY generation_id`, scope)
	if err != nil {
		c.t.Fatalf("read work item states: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var gen, status string
		var attempts int
		if err := rows.Scan(&gen, &status, &attempts); err != nil {
			c.t.Fatal(err)
		}
		out = append(out, gen+" status="+status)
	}
	if err := rows.Err(); err != nil {
		c.t.Fatalf("iterate work item states: %v", err)
	}
	return out
}

// TestRekeyOscillationConvergesLive is the criterion-7 proof: the alias
// add→remove→add oscillation across four generations converges to the
// original keys each time, with no duplicate rows and no dead-lettered work.
func TestRekeyOscillationConvergesLive(t *testing.T) {
	c := newRekeyProof(t)
	c.seedOscillationPair()
	c.drain()
	assertOscillation := func(wantKeys []string, wantRows int, stage string) {
		t.Helper()
		if got := c.keysByScope(rekeyCommitter); len(got) != len(wantKeys) {
			t.Fatalf("%s keys = %v, want %v", stage, got, wantKeys)
		} else {
			for i := range wantKeys {
				if got[i] != wantKeys[i] {
					t.Fatalf("%s keys = %v, want %v", stage, got, wantKeys)
				}
			}
		}
		if got := c.countFindingRows(rekeyCommitter); got != wantRows {
			t.Fatalf("%s finding rows = %d, want %d (no duplicates)", stage, got, wantRows)
		}
		if got := c.pendingWorkItems(rekeyCommitter); got != 0 {
			t.Fatalf("%s pending work items = %d, want 0 (no dead-letter on convergence)", stage, got)
		}
	}
	assertOscillation([]string{carryOscA, carryOscB}, 2, "g1 lone groups")

	// Each rescan seeds the new generation's item via copyGeneration; no
	// rerun is needed (rerunning would also revive the superseded
	// generation's item, which is L2's pattern, not this criterion's).
	c.rescanOscillation("g1", "g2", []string{carryOscA, carryOscB})
	c.drain()
	t.Logf("g2 findings: %v", c.findingKeys())
	assertOscillation([]string{carryOscA}, 2, "g2 merged")

	c.rescanOscillation("g2", "g3", []string{carryOscA})
	c.drain()
	t.Logf("g3 findings: %v", c.findingKeys())
	assertOscillation([]string{carryOscA, carryOscB}, 2, "g3 split back")

	c.rescanOscillation("g3", "g4", []string{carryOscA, carryOscB})
	c.drain()
	t.Logf("g4 findings: %v", c.findingKeys())
	assertOscillation([]string{carryOscA}, 2, "g4 re-merged")

	// Steady state: replaying the same alias generation re-keys nothing.
	c.rerun(rekeyCommitter)
	c.handled = nil
	replay := c.drainCapturing()
	for _, item := range c.workItemStates(rekeyCommitter) {
		t.Logf("item: %s", item)
	}
	assertOscillation([]string{carryOscA}, 2, "g4 replay")
	if got := replay[rekeyCommitter+"@"+fanOutGen(rekeyCommitter, "g4")].SubSignals["suppression_orphaned_by_split"]; got != 0 {
		t.Fatalf("replay orphaned = %v, want 0 (no group change, no orphan)", got)
	}
}
