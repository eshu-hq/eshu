// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8827 disposable PostgreSQL 18 proof that readiness/producer.ImpactStore counts only the requested scope generation's supply_chain_impact rows (other generations, other scopes, and sibling domains never count), reports the last-workable anchor over in-flight rows only, and that its plan reaches fact_work_items through fact_work_items_scope_generation_idx with no sequential scan on a 350,000-row backlog; enrolled in the live-postgres-readiness runner and the reducer contention gate. Local run about 6s on postgres:18; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	producerreadiness "github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/producer"
)

// Live proofs for readiness/producer.ImpactStore (#8827). They live in this
// package so the TestReducerContentionGate prefix enrolls them in the reducer
// contention gate. Locally:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run ReducerContentionGateSecurityAlertImpact -count=1 -v

// seedImpactProducerRow inserts one reducer work item with the given status and
// optional last_attempt_at.
func seedImpactProducerRow(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	id, scopeID, generationID, domain, status string,
	lastAttemptAt *time.Time,
) {
	t.Helper()
	now := time.Now().UTC().Add(-time.Hour)
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
                             status, payload, created_at, updated_at, last_attempt_at)
VALUES ($1, $2, $3, 'reducer', $4, 'scope', $2, $5, '{}'::jsonb, $6, $6, $7)`,
		id, scopeID, generationID, domain, status, now, lastAttemptAt); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func seedImpactProducerScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID string, generations ...string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
	        collector_kind, partition_key, observed_at, ingested_at, status, payload)
	      VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', '{}'::jsonb)`, scopeID, now); err != nil {
		t.Fatalf("seed scope %s: %v", scopeID, err)
	}
	for i, generation := range generations {
		status := "pending" // a scope has at most one active generation
		if i == 0 {
			status = "active"
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind,
		        observed_at, ingested_at, status) VALUES ($1, $2, 'snapshot', $3, $3, $4)`,
			generation, scopeID, now, status); err != nil {
			t.Fatalf("seed generation %s: %v", generation, err)
		}
	}
}

// TestReducerContentionGateSecurityAlertImpactProducerStateScopesToTheGeneration
// pins the lifecycle semantics against the real schema: only the requested
// scope generation's supply_chain_impact rows count; a sibling domain at the
// same generation, the same domain at another generation, and another scope's
// row never do; and the anchor moves only for in-flight rows.
func TestReducerContentionGateSecurityAlertImpactProducerStateScopesToTheGeneration(t *testing.T) {
	sqlDB, ctx := runtimeProducerStoreLiveDB(t)
	store := producerreadiness.ImpactStore{DB: SQLDB{DB: sqlDB}}
	attempt := time.Now().UTC().Add(-10 * time.Minute)

	seedImpactProducerScope(t, ctx, sqlDB, "scope-impact-a", "gen-a1", "gen-a2")
	seedImpactProducerScope(t, ctx, sqlDB, "scope-impact-b", "gen-b1")
	seedImpactProducerRow(t, ctx, sqlDB, "a1-retrying", "scope-impact-a", "gen-a1", "supply_chain_impact", "retrying", &attempt)
	seedImpactProducerRow(t, ctx, sqlDB, "a1-done", "scope-impact-a", "gen-a1", "supply_chain_impact", "succeeded", nil)
	seedImpactProducerRow(t, ctx, sqlDB, "a1-sibling", "scope-impact-a", "gen-a1", "security_alert_reconciliation", "pending", nil)
	seedImpactProducerRow(t, ctx, sqlDB, "a2-pending", "scope-impact-a", "gen-a2", "supply_chain_impact", "pending", nil)
	seedImpactProducerRow(t, ctx, sqlDB, "b1-claimed", "scope-impact-b", "gen-b1", "supply_chain_impact", "claimed", nil)

	got, err := store.ImpactProducerState(ctx, "scope-impact-a", "gen-a1")
	if err != nil {
		t.Fatalf("ImpactProducerState() error = %v", err)
	}
	if got.Retrying != 1 || got.Pending != 0 || got.Claimed != 0 || got.Running != 0 || got.Total != 2 || got.InFlight() != 1 {
		t.Fatalf("scope-impact-a/gen-a1 = %+v, want 1 retrying of 2 total (no sibling, other-generation, or other-scope rows)", got)
	}
	if got.NewestInFlightWorkableAt.IsZero() || got.NewestInFlightWorkableAt.Before(attempt.Add(-time.Second)) {
		t.Fatalf("anchor = %s, want at least the retrying row's last attempt %s", got.NewestInFlightWorkableAt, attempt)
	}

	terminalOnly, err := store.ImpactProducerState(ctx, "scope-impact-b", "gen-b9")
	if err != nil {
		t.Fatalf("ImpactProducerState() absent error = %v", err)
	}
	if terminalOnly.Total != 0 || terminalOnly.InFlight() != 0 || !terminalOnly.NewestInFlightWorkableAt.IsZero() {
		t.Fatalf("absent generation = %+v, want the zero state", terminalOnly)
	}

	if _, err := sqlDB.ExecContext(ctx, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id = 'a1-retrying'`); err != nil {
		t.Fatalf("finish retrying row: %v", err)
	}
	done, err := store.ImpactProducerState(ctx, "scope-impact-a", "gen-a1")
	if err != nil {
		t.Fatalf("ImpactProducerState() terminal error = %v", err)
	}
	if done.InFlight() != 0 || done.Total != 2 || !done.NewestInFlightWorkableAt.IsZero() {
		t.Fatalf("terminal-only = %+v, want 0 in flight, total 2, NULL anchor", done)
	}
}

// TestReducerContentionGateSecurityAlertImpactProducerStatePlanUsesScopeGenerationIndex
// is the plan guard on a populated backlog: 2,000 scopes x 5 generations x 35
// domains = 350,000 reducer rows. The probe must read only the one scope
// generation's rows through fact_work_items_scope_generation_idx, never a
// sequential scan; the ANALYZE plan is logged for the PR evidence.
func TestReducerContentionGateSecurityAlertImpactProducerStatePlanUsesScopeGenerationIndex(t *testing.T) {
	sqlDB, ctx := runtimeProducerStoreLiveDB(t)
	for _, statement := range []string{
		`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
                              observed_at, ingested_at, status, payload)
SELECT 'plan8827-' || s, 'repository', 'git', 'plan8827-' || s, 'git', 'p', now(), now(), 'active', '{}'::jsonb
FROM generate_series(1, 2000) AS s`, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'plan8827-' || s || '-g' || g, 'plan8827-' || s, 'snapshot', now(), now(),
       CASE WHEN g = 5 THEN 'active' ELSE 'superseded' END
FROM generate_series(1, 2000) AS s CROSS JOIN generate_series(1, 5) AS g`, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
                             status, payload, created_at, updated_at)
SELECT 'plan8827-' || s || '-g' || g || '-' || d, 'plan8827-' || s, 'plan8827-' || s || '-g' || g, 'reducer',
       CASE d WHEN 0 THEN 'supply_chain_impact' WHEN 1 THEN 'security_alert_reconciliation' ELSE 'domain_' || d END,
       'scope', 'plan8827-' || s || '-g' || g || '-' || d,
       CASE WHEN d = 0 AND g = 5 AND s % 10 = 0 THEN 'pending' ELSE 'succeeded' END, '{}'::jsonb, now(), now()
FROM generate_series(1, 2000) AS s CROSS JOIN generate_series(1, 5) AS g CROSS JOIN generate_series(0, 34) AS d`,
	} {
		if _, err := sqlDB.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed plan rows: %v", err)
		}
	}
	for _, table := range []string{"ingestion_scopes", "scope_generations", "fact_work_items"} {
		if _, err := sqlDB.ExecContext(ctx, "ANALYZE "+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
	var rows int
	if err := sqlDB.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items WHERE work_item_id LIKE 'plan8827-%'`).Scan(&rows); err != nil || rows != 350000 {
		t.Fatalf("seeded rows = %d, err = %v, want 350000", rows, err)
	}

	var planJSON string
	if err := sqlDB.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+producerreadiness.ImpactProducerStateQuery,
		"plan8827-10", "plan8827-10-g5", "supply_chain_impact").Scan(&planJSON); err != nil {
		t.Fatalf("explain: %v", err)
	}
	var plan []map[string]any
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if seqScanOn(plan[0]["Plan"], "fact_work_items") {
		t.Fatalf("impact producer probe sequentially scans fact_work_items:\n%s", planJSON)
	}
	if !strings.Contains(planJSON, "fact_work_items_scope_generation_idx") {
		t.Fatalf("impact producer probe does not use fact_work_items_scope_generation_idx:\n%s", planJSON)
	}
	top, _ := plan[0]["Plan"].(map[string]any)
	t.Logf("plan (350,000 rows): planning=%v ms execution=%v ms shared_hit_blocks=%v shared_read_blocks=%v",
		plan[0]["Planning Time"], plan[0]["Execution Time"], top["Shared Hit Blocks"], top["Shared Read Blocks"])

	state, err := producerreadiness.ImpactStore{DB: SQLDB{DB: sqlDB}}.ImpactProducerState(ctx, "plan8827-10", "plan8827-10-g5")
	if err != nil || state.Pending != 1 || state.Total != 1 {
		t.Fatalf("ImpactProducerState() = %+v, %v; want 1 pending of 1", state, err)
	}
}
