// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Helpers for the D7 identity-gate proofs (#10972): a scope's finding rows by
// content digest, the winners staging watermark, and the fact marks staged
// after it.

// gateFindingDigests returns a scope's active finding rows keyed by fact id,
// valued by the writer's full content digest.
func (c *convergence) gateFindingDigests(scope string) map[string]string {
	c.t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT fact_id, COALESCE(payload #>> '{provenance,content_digest}', '') FROM fact_records
WHERE scope_id = $1 AND fact_kind = $2 AND NOT is_tombstone`, scope, facts.ReducerSupplyChainImpactFindingFactKind)
	if err != nil {
		c.t.Fatalf("read finding digests: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var id, digest string
		if err := rows.Scan(&id, &digest); err != nil {
			c.t.Fatal(err)
		}
		out[id] = digest
	}
	if err := rows.Err(); err != nil {
		c.t.Fatal(err)
	}
	return out
}

// gateStagingWatermark returns the newest winners staging sequence.
func (c *convergence) gateStagingWatermark() int64 {
	c.t.Helper()
	var seq int64
	if err := c.db.QueryRowContext(c.ctx, `SELECT COALESCE(max(seq), 0) FROM supply_chain_impact_winners_dirty_staging`).Scan(&seq); err != nil {
		c.t.Fatalf("read staging watermark: %v", err)
	}
	return seq
}

// gateStagedFactMarks counts the impact writer's fact marks staged after a
// watermark.
func (c *convergence) gateStagedFactMarks(after int64) int {
	c.t.Helper()
	var n int
	if err := c.db.QueryRowContext(c.ctx, `
SELECT count(*) FROM supply_chain_impact_winners_dirty_staging
WHERE seq > $1 AND kind = 'fact' AND source IN ('fact_upsert', 'fact_retract')`, after).Scan(&n); err != nil {
		c.t.Fatalf("count staged marks: %v", err)
	}
	return n
}

// gateChangedKeys lists the keys whose value differs between two snapshots.
func gateChangedKeys(before, after map[string]string) []string {
	var out []string
	for key, value := range before {
		if got, ok := after[key]; !ok || got != value {
			out = append(out, key)
		}
	}
	for key := range after {
		if _, ok := before[key]; !ok {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
