// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	producerreadiness "github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/producer"
)

// Live proofs for readiness/producer.Store (#7816). They live in this
// package so the TestReducerContentionGate prefix enrolls them in the reducer
// contention gate. Locally:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run ReducerContentionGateRuntimeProducerStates -count=1 -v
func runtimeProducerStoreLiveDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the runtime producer readiness live proofs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	return db, ctx
}

// seedRuntimeProducerScope registers one repository scope with an older superseded,
// an active, and a newer pending generation, and one workload_materialization
// row per generation in the given statuses.
func seedRuntimeProducerScope(t *testing.T, ctx context.Context, sqlDB *sql.DB, repoID, older, active, newer string) {
	t.Helper()
	scopeID := "git-repository-scope:" + repoID
	now := time.Now().UTC()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := sqlDB.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %s: %v", repoID, err)
		}
	}
	exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
	        partition_key, observed_at, ingested_at, status, payload)
	      VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', '{}'::jsonb)`, scopeID, now)
	for i, gen := range []struct{ status, work string }{{"superseded", older}, {"active", active}, {"pending", newer}} {
		generationID := fmt.Sprintf("%s-gen-%d", repoID, i)
		exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
		      VALUES ($1, $2, 'snapshot', $3, $3, $4)`, generationID, scopeID, now.Add(time.Duration(i)*time.Minute), gen.status)
		if gen.status == "active" {
			exec(`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, generationID, scopeID)
		}
		if gen.work == "" {
			continue
		}
		exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
		        conflict_key, status, payload, created_at, updated_at)
		      VALUES ($1, $2, $3, 'reducer', 'workload_materialization', 'platform_graph', $1, $4, '{}'::jsonb, $5, $5)`,
			"wm-"+generationID, scopeID, generationID, gen.work, now)
		// A sibling domain at the same generation must never be counted.
		exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
		        conflict_key, status, payload, created_at, updated_at)
		      VALUES ($1, $2, $3, 'reducer', 'code_call_materialization', 'code_graph', $1, 'pending', '{}'::jsonb, $4, $4)`,
			"cc-"+generationID, scopeID, generationID, now)
	}
}

// TestReducerContentionGateRuntimeProducerStatesActiveGenerationOnly pins the lifecycle
// semantics against the real schema: only the ACTIVE generation's
// workload_materialization rows count; an older generation's live row, a newer
// pending generation's row, and another domain's row never do.
func TestReducerContentionGateRuntimeProducerStatesActiveGenerationOnly(t *testing.T) {
	sqlDB, ctx := runtimeProducerStoreLiveDB(t)
	suffix := time.Now().UnixNano()
	inFlight := fmt.Sprintf("repository:r_live_inflight_%d", suffix)
	terminal := fmt.Sprintf("repository:r_live_terminal_%d", suffix)
	newerOnly := fmt.Sprintf("repository:r_live_newer_%d", suffix)
	absent := fmt.Sprintf("repository:r_live_absent_%d", suffix)
	seedRuntimeProducerScope(t, ctx, sqlDB, inFlight, "succeeded", "claimed", "pending")
	seedRuntimeProducerScope(t, ctx, sqlDB, terminal, "running", "succeeded", "")
	seedRuntimeProducerScope(t, ctx, sqlDB, newerOnly, "", "", "pending")

	states, err := producerreadiness.Store{DB: SQLDB{DB: sqlDB}}.RuntimeProducerStates(ctx,
		[]string{inFlight, terminal, newerOnly, absent},
		[]reducercontract.Domain{reducercontract.DomainWorkloadMaterialization})
	if err != nil {
		t.Fatalf("RuntimeProducerStates() error = %v", err)
	}
	want := map[string]servicecatalog.RuntimeProducerState{
		inFlight: {Claimed: 1, Total: 1},
		terminal: {Total: 1},
	}
	if !reflect.DeepEqual(states, want) {
		t.Fatalf("states = %+v, want %+v", states, want)
	}
}

// TestReducerContentionGateRuntimeProducerStatesPlanUsesScopeGenerationIndex is the plan-shape
// guard: on a populated table the lookup reads fact_work_items through
// fact_work_items_scope_generation_idx and never sequentially scans it.
func TestReducerContentionGateRuntimeProducerStatesPlanUsesScopeGenerationIndex(t *testing.T) {
	sqlDB, ctx := runtimeProducerStoreLiveDB(t)
	prefix := fmt.Sprintf("repository:r_plan_%d_", time.Now().UnixNano())
	for _, statement := range []string{
		`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
                              observed_at, ingested_at, status, payload)
SELECT 'git-repository-scope:' || $1 || s, 'repository', 'git', $1 || s, 'git', 'p', now(), now(), 'active', '{}'::jsonb
FROM generate_series(1, 600) AS s`, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT $1 || s || '-gen', 'git-repository-scope:' || $1 || s, 'snapshot', now(), now(), 'active'
FROM generate_series(1, 600) AS s`, `
UPDATE ingestion_scopes SET active_generation_id = substring(scope_id from 22) || '-gen'
WHERE scope_id LIKE 'git-repository-scope:' || $1 || '%'`, `
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
                             status, payload, created_at, updated_at)
SELECT $1 || s || '-' || d, 'git-repository-scope:' || $1 || s, $1 || s || '-gen', 'reducer',
       CASE d WHEN 0 THEN 'workload_materialization' ELSE 'domain_' || d END, 'scope', $1 || s || '-' || d,
       'succeeded', '{}'::jsonb, now(), now()
FROM generate_series(1, 600) AS s CROSS JOIN generate_series(0, 34) AS d`,
	} {
		if _, err := sqlDB.ExecContext(ctx, statement, prefix); err != nil {
			t.Fatalf("seed plan rows: %v", err)
		}
	}
	for _, table := range []string{"ingestion_scopes", "scope_generations", "fact_work_items"} {
		if _, err := sqlDB.ExecContext(ctx, "ANALYZE "+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
	var planJSON string
	if err := sqlDB.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+producerreadiness.RuntimeProducerStatesQuery,
		[]string{"git-repository-scope:" + prefix + "7", "git-repository-scope:" + prefix + "300"},
		[]string{string(reducercontract.DomainWorkloadMaterialization)},
	).Scan(&planJSON); err != nil {
		t.Fatalf("explain: %v", err)
	}
	var plan []map[string]any
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if seqScanOn(plan[0]["Plan"], "fact_work_items") {
		t.Fatalf("readiness lookup sequentially scans fact_work_items:\n%s", planJSON)
	}
	if !strings.Contains(planJSON, "fact_work_items_scope_generation_idx") {
		t.Fatalf("readiness lookup does not use fact_work_items_scope_generation_idx:\n%s", planJSON)
	}
}

func seqScanOn(node any, relation string) bool {
	m, ok := node.(map[string]any)
	if !ok {
		return false
	}
	if m["Node Type"] == "Seq Scan" && m["Relation Name"] == relation {
		return true
	}
	children, _ := m["Plans"].([]any)
	for _, child := range children {
		if seqScanOn(child, relation) {
			return true
		}
	}
	return false
}
