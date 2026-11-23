// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// queuedPlanViolation returns why plan is not an accepted prefilter plan, or ""
// when it is. The contract is that the queue table is never read by a
// sequential scan: the cost has to stay bounded by an index, not by the table.
// Two indexes are accepted, because the planner correctly chooses between them
// by the size of the holder list:
//
//   - fact_work_items_scope_generation_idx: a per-scope probe, best when the
//     list is short against a large queue.
//   - fact_work_items_stage_domain_status_idx: one range read of the domain's
//     matched-status rows with the holder list applied as a filter, best when
//     the list is a large share of those rows. On the QA replica the largest
//     stored list (4,013 holders) takes this plan: 4,017 rows kept, 5,029
//     removed by the filter, so the cost follows the domain's matched-status
//     rows (about 9k), not the holder count.
//
// Any other plan, including one with no index on fact_work_items, is refused so
// a new plan shape is looked at before it ships.
func queuedPlanViolation(plan string) string {
	if strings.Contains(plan, "Seq Scan on fact_work_items") {
		return "sequential scan of fact_work_items"
	}
	if !strings.Contains(plan, "fact_work_items_scope_generation_idx") &&
		!strings.Contains(plan, "fact_work_items_stage_domain_status_idx") {
		return "fact_work_items is read through neither fact_work_items_scope_generation_idx nor fact_work_items_stage_domain_status_idx"
	}
	return ""
}

// TestFindQueuedHolderScopesPlanAvoidsQueueSeqScanLive explains the shipped
// prefilter over a queue where the asked holders are a small minority of the
// rows. The prefilter must read the queue through an index, never a sequential
// scan; queuedPlanViolation names the two accepted shapes. The plan is logged
// so the QA replica EXPLAIN can be compared with it.
func TestFindQueuedHolderScopesPlanAvoidsQueueSeqScanLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	const scopes = 4000
	for _, statement := range []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
SELECT 'plan-' || n, 'vulnerability_intelligence', 'synthetic', 'plan-' || n, 'synthetic', 'plan-' || n, now(), now(), 'active', '{}'::jsonb
FROM generate_series(1, $1) AS n`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
SELECT 'plan-' || n || '/g1', 'plan-' || n, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb
FROM generate_series(1, $1) AS n`,
		`UPDATE ingestion_scopes SET active_generation_id = scope_id || '/g1' WHERE scope_id LIKE 'plan-%' AND $1 > 0`,
		`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key, status, attempt_count, payload, created_at, updated_at)
SELECT 'plan-item-' || n, 'plan-' || n, 'plan-' || n || '/g1', 'reducer', 'supply_chain_impact', 'scope', 'plan-' || n,
       CASE WHEN n % 3 = 0 THEN 'pending' WHEN n % 3 = 1 THEN 'succeeded' ELSE 'retrying' END, 0,
       CASE WHEN n % 2 = 0 THEN jsonb_build_object('fanout_origin', jsonb_build_object('scope_id', 'src', 'cves', jsonb_build_array('CVE-2026-' || n)))
            ELSE '{}'::jsonb END, now(), now()
FROM generate_series(1, $1) AS n`,
	} {
		if _, err := db.ExecContext(ctx, statement, scopes); err != nil {
			t.Fatalf("seed plan fixture: %v", err)
		}
	}
	for _, table := range []string{"ingestion_scopes", "scope_generations", "fact_work_items"} {
		if _, err := db.ExecContext(ctx, "ANALYZE "+table); err != nil {
			t.Fatal(err)
		}
	}
	holders := make([]string, 0, 300)
	for n := 1; n <= 300; n++ {
		holders = append(holders, fmt.Sprintf("plan-%d", n*13))
	}
	// Half the rows carry a bounded stamp, so the cover arms read payload the
	// way they do on QA; the change is bounded, so every arm is evaluated.
	rows, err := db.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+postgres.FindQueuedSupplyChainImpactHoldersQueryForTest,
		holders, `["CVE-2026-7002"]`, true)
	if err != nil {
		t.Fatalf("explain prefilter: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("prefilter plan over %d queue rows, %d holders:\n%s", scopes, len(holders), plan.String())
	if why := queuedPlanViolation(plan.String()); why != "" {
		t.Fatalf("prefilter plan is not an accepted shape: %s\n%s", why, plan.String())
	}
}
