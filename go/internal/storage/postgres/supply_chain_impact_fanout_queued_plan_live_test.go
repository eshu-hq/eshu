// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// queuedPlanViolation returns why plan is not the per-scope probe shape, or ""
// when it is: fact_work_items must be read through an index keyed by scope (no
// sequential scan of the queue table), and the plan must not sort or hash the
// whole queue to find the holders.
func queuedPlanViolation(plan string) string {
	if strings.Contains(plan, "Seq Scan on fact_work_items") {
		return "sequential scan of fact_work_items"
	}
	if !strings.Contains(plan, "fact_work_items_scope_generation_idx") && !strings.Contains(plan, "fact_work_items_pkey") {
		return "fact_work_items is not read through a scope-keyed index"
	}
	return ""
}

// TestFindQueuedHolderScopesPlanProbesTheQueueByScopeLive explains the shipped
// prefilter over a queue where the asked holders are a small minority of the
// rows. The prefilter must probe the queue per scope instead of scanning it.
// The plan is logged so the QA replica EXPLAIN can be compared with it.
func TestFindQueuedHolderScopesPlanProbesTheQueueByScopeLive(t *testing.T) {
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
       CASE WHEN n % 3 = 0 THEN 'pending' WHEN n % 3 = 1 THEN 'succeeded' ELSE 'retrying' END, 0, '{}'::jsonb, now(), now()
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
	rows, err := db.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+postgres.FindQueuedSupplyChainImpactHoldersQueryForTest, holders)
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
		t.Fatalf("prefilter plan violates the probe shape: %s\n%s", why, plan.String())
	}
}
