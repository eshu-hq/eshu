// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7816 disposable PostgreSQL 18 proof that readiness/producer.Store counts only the active generation's workload_materialization rows (older, newer pending, and sibling-domain rows never count), returns the newest in-flight last-workable time, max(GREATEST(COALESCE(reopened_at, created_at), COALESCE(last_attempt_at, created_at), COALESCE(visible_at, created_at), COALESCE(claim_until, created_at))), and the newest claim_until over in-flight rows only, and that its plan reaches fact_work_items through fact_work_items_scope_generation_idx with no sequential scan; enrolled in the live-postgres-readiness runner and the reducer contention gate. Local run about 2.5s on postgres:18; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

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
	if states[inFlight].NewestInFlightWorkableAt.IsZero() {
		t.Fatalf("in-flight repository has no producer workable anchor: %+v", states[inFlight])
	}
	if anchor := states[terminal].NewestInFlightWorkableAt; !anchor.IsZero() {
		t.Fatalf("terminal-only repository anchor = %s, want zero (NULL): no row is in flight", anchor)
	}
	counts := make(map[string]servicecatalog.RuntimeProducerState, len(states))
	for repoID, state := range states {
		state.NewestInFlightWorkableAt = time.Time{}
		counts[repoID] = state
	}
	want := map[string]servicecatalog.RuntimeProducerState{
		inFlight: {Claimed: 1, Total: 1},
		terminal: {Total: 1},
	}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("states = %+v, want %+v", counts, want)
	}
}

// TestReducerContentionGateRuntimeProducerStatesNewestInFlightWorkableAt pins
// the elapsed-bound anchor the fence reads (#7816 amendments A2 and A3): per
// repository, the latest last-workable time across in-flight
// active-generation producer rows, where a row's last-workable time is
// GREATEST(COALESCE(reopened_at, created_at), COALESCE(last_attempt_at,
// created_at), COALESCE(visible_at, created_at), COALESCE(claim_until,
// created_at)). A terminal row and a newer pending generation never move it;
// a claim, a scheduled retry, a reopen, and a heartbeated lease each do, and
// a sleeping retry or a live lease anchors in the future. It also pins the
// newest in-flight claim_until the log line reports: the mixed repository
// holds two claimed rows, claimed through the real queue on two clocks, and
// the column must report the later lease end, not the earlier one.
func TestReducerContentionGateRuntimeProducerStatesNewestInFlightWorkableAt(t *testing.T) {
	sqlDB, ctx := runtimeProducerStoreLiveDB(t)
	suffix := time.Now().UnixNano()
	mixed := fmt.Sprintf("repository:r_anchor_mixed_%d", suffix)
	reopened := fmt.Sprintf("repository:r_anchor_reopened_%d", suffix)
	sleeping := fmt.Sprintf("repository:r_anchor_sleeping_%d", suffix)
	heartbeating := fmt.Sprintf("repository:r_anchor_heartbeating_%d", suffix)
	// The mixed repository's claimed rows come from the real Claim path, so
	// they are claimed before any other workload_materialization row exists
	// in this schema: the scope starts with only its superseded row.
	seedRuntimeProducerScope(t, ctx, sqlDB, mixed, "succeeded", "", "")
	mixedScope, mixedGen := "git-repository-scope:"+mixed, mixed+"-gen-1"
	claimNow := time.Now().UTC()
	leaseEnds := make([]time.Time, 0, 2)
	for i, claim := range []struct {
		owner      string
		claimedAgo time.Duration
	}{
		// Two dead workers: claimed 15 and 13 minutes ago with a 1-minute
		// lease, so the leases ended 14 and 12 minutes ago.
		{"store-worker-a", 15 * time.Minute},
		{"store-worker-b", 13 * time.Minute},
	} {
		id := fmt.Sprintf("wm%d-%s", 3+2*i, mixedGen)
		if _, err := sqlDB.ExecContext(ctx, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage,
		        domain, conflict_domain, conflict_key, status, payload, created_at, updated_at)
		      VALUES ($1, $2, $3, 'reducer', 'workload_materialization', 'platform_graph', $1, 'pending', '{}'::jsonb, $4, $4)`,
			id, mixedScope, mixedGen, claimNow.Add(-time.Hour)); err != nil {
			t.Fatalf("enqueue %s: %v", id, err)
		}
		clock := claimNow.Add(-claim.claimedAgo)
		queue := runtimeFenceQueue(sqlDB, claim.owner, reducercontract.DomainWorkloadMaterialization,
			func() time.Time { return clock })
		intent, claimed, err := queue.Claim(ctx)
		if err != nil || !claimed || intent.IntentID != id {
			t.Fatalf("%s Claim() = %q, %v, %v, want %s claimed", claim.owner, intent.IntentID, claimed, err, id)
		}
		var leaseEnd time.Time
		if err := sqlDB.QueryRowContext(ctx, `SELECT claim_until FROM fact_work_items WHERE work_item_id = $1`, id).
			Scan(&leaseEnd); err != nil {
			t.Fatalf("read %s claim_until: %v", id, err)
		}
		leaseEnds = append(leaseEnds, leaseEnd)
	}
	if !leaseEnds[1].After(leaseEnds[0]) {
		t.Fatalf("lease ends = %v, want the second claim's lease to end later", leaseEnds)
	}
	// Now the rest of the mixed repository: its active pending row and a row
	// of a newer pending generation, as the seed would have written them.
	for _, row := range []struct{ id, gen string }{{"wm-" + mixedGen, mixedGen}, {"wm-" + mixed + "-gen-2", mixed + "-gen-2"}} {
		if _, err := sqlDB.ExecContext(ctx, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage,
		        domain, conflict_domain, conflict_key, status, payload, created_at, updated_at)
		      VALUES ($1, $2, $3, 'reducer', 'workload_materialization', 'platform_graph', $1, 'pending', '{}'::jsonb, now(), now())`,
			row.id, mixedScope, row.gen); err != nil {
			t.Fatalf("enqueue %s: %v", row.id, err)
		}
	}
	seedRuntimeProducerScope(t, ctx, sqlDB, reopened, "", "pending", "")
	seedRuntimeProducerScope(t, ctx, sqlDB, sleeping, "", "pending", "")
	seedRuntimeProducerScope(t, ctx, sqlDB, heartbeating, "", "pending", "")
	base := time.Now().UTC().Truncate(time.Microsecond)
	ago := func(d time.Duration) time.Time { return base.Add(-d) }
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := sqlDB.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("shape anchor rows: %v", err)
		}
	}
	// Active pending row: enqueued 40 minutes ago, never claimed.
	exec(`UPDATE fact_work_items SET created_at = $2, updated_at = $2 WHERE work_item_id = $1`,
		"wm-"+mixedGen, ago(40*time.Minute))
	// Two more rows at the same generation, shaped like the queue writes
	// them: a retrying row (claimed 20 min ago, retry visible 10 min ago,
	// lease cleared) and a terminal row (1 min ago) that must not count.
	for _, row := range []struct {
		id, status                        string
		created, attempt, visible, cuntil any
	}{
		{"wm2-" + mixedGen, "retrying", ago(50 * time.Minute), ago(20 * time.Minute), ago(10 * time.Minute), nil},
		{"wm4-" + mixedGen, "succeeded", ago(time.Minute), ago(time.Minute), nil, nil},
	} {
		exec(`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
		        conflict_key, status, payload, created_at, updated_at, last_attempt_at, visible_at, claim_until)
		      VALUES ($1, $2, $3, 'reducer', 'workload_materialization', 'platform_graph', $1, $4, '{}'::jsonb, $5, $5, $6, $7, $8)`,
			row.id, mixedScope, mixedGen, row.status, row.created, row.attempt, row.visible, row.cuntil)
	}
	// The reopened repository's row was enqueued two hours ago, last claimed
	// 90 minutes ago, and reopened (reopened_at and visible_at) 5 minutes ago.
	exec(`UPDATE fact_work_items SET created_at = $2, last_attempt_at = $3, reopened_at = $4, visible_at = $4, updated_at = $4
	      WHERE work_item_id = $1`,
		"wm-"+reopened+"-gen-1", ago(2*time.Hour), ago(90*time.Minute), ago(5*time.Minute))
	// The sleeping repository's row is in a retry backoff that ends in 10
	// minutes; its last attempt was 1 minute ago.
	exec(`UPDATE fact_work_items SET status = 'retrying', created_at = $2, last_attempt_at = $3, visible_at = $4,
	        next_attempt_at = $4, updated_at = $3 WHERE work_item_id = $1`,
		"wm-"+sleeping+"-gen-1", ago(45*time.Minute), ago(time.Minute), base.Add(10*time.Minute))
	// The heartbeating repository's row was claimed 45 minutes ago and its
	// worker still heartbeats: the lease ends 1 minute from now.
	exec(`UPDATE fact_work_items SET status = 'running', lease_owner = 'worker', created_at = $2, last_attempt_at = $3,
	        claim_until = $4, updated_at = $3 WHERE work_item_id = $1`,
		"wm-"+heartbeating+"-gen-1", ago(time.Hour), ago(45*time.Minute), base.Add(time.Minute))

	states, err := producerreadiness.Store{DB: SQLDB{DB: sqlDB}}.RuntimeProducerStates(ctx,
		[]string{mixed, reopened, sleeping, heartbeating}, []reducercontract.Domain{reducercontract.DomainWorkloadMaterialization})
	if err != nil {
		t.Fatalf("RuntimeProducerStates() error = %v", err)
	}
	for repoID, want := range map[string]time.Time{
		mixed:        ago(10 * time.Minute),
		reopened:     ago(5 * time.Minute),
		sleeping:     base.Add(10 * time.Minute),
		heartbeating: base.Add(time.Minute),
	} {
		if got := states[repoID].NewestInFlightWorkableAt; !got.Equal(want) {
			t.Fatalf("%s anchor = %s, want %s (state %+v)", repoID, got, want, states[repoID])
		}
	}
	for repoID, want := range map[string]time.Time{
		mixed:        leaseEnds[1],
		reopened:     {},
		sleeping:     {},
		heartbeating: base.Add(time.Minute),
	} {
		if got := states[repoID].NewestInFlightClaimUntil; !got.Equal(want) {
			t.Fatalf("%s newest claim_until = %s, want %s", repoID, got, want)
		}
	}
	if got := states[mixed]; got.Pending != 1 || got.Retrying != 1 || got.Claimed != 2 || got.Total != 5 {
		t.Fatalf("mixed counts = %+v, want pending 1, retrying 1, claimed 2, total 5", got)
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
