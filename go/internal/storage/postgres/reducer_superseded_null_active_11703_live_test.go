// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11703 disposable PostgreSQL 18 proof that Claim, ClaimBatch, SupersedeStaleReducerGenerations, and active_fact_work_items fence, hide, and sweep unleased reducer work items on retired scopes (active_generation_id IS NULL and scope_generations.status = 'superseded') while sparing failed/pending generations, dangling active pointers, claimed/projector rows, and fan-out-debt continuations, plus EvalPlanQual recheck and concurrent SKIP LOCKED sweep safety; isolated schemas; about 4 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DISPOSABLE ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

func seedNullActiveScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID, activeGenID, genID, genStatus string, now time.Time) {
	t.Helper()
	var active any
	if activeGenID != "" {
		active = activeGenID
	}
	mustExecGateArgs(t, ctx, db, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, 'repository', 'osv', $1, NULL, 'osv', $1, $2, $2, 'active', $3, '{}'::jsonb)`,
		scopeID, now, active)
	var supersededAt any
	if genStatus == "superseded" {
		supersededAt = now.Add(time.Minute)
	}
	mustExecGateArgs(t, ctx, db, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ($1, $2, 'snapshot', 'null-active', $3, $3, $4, $3, $5, '{}'::jsonb)`,
		genID, scopeID, now, genStatus, supersededAt)
}

func insertNullActiveWorkItem(t *testing.T, ctx context.Context, db *sql.DB, id, scopeID, genID, stage, domain, status, payload, failureClass string, now time.Time) {
	t.Helper()
	var visibleAt, claimUntil, leaseOwner, fc any
	if status == "retrying" {
		visibleAt = now.Add(-time.Minute)
	}
	if status == "claimed" || status == "running" {
		leaseOwner = "live-worker"
		claimUntil = now.Add(time.Hour)
	}
	if failureClass != "" {
		fc = failureClass
	}
	if payload == "" {
		payload = `{}`
	}
	mustExecGateArgs(t, ctx, db, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, lease_owner, claim_until, visible_at,
    failure_class, payload, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, 'scope', $1, $6, 0, $7, $8, $9, $10, $11::jsonb, $12, $12)`,
		id, scopeID, genID, stage, domain, status, leaseOwner, claimUntil, visibleAt, fc, payload, now)
}

// TestReducerSupersededNullActiveClaimAndSweepLive is the #11703 regression
// proving Claim, ClaimBatch, and SupersedeStaleReducerGenerations fence and
// terminalize unleased reducer items on a retired scope
// (active_generation_id IS NULL and scope_generations.status = 'superseded')
// while sparing failed/pending generations, dangling active pointers,
// claimed/projector rows, and supply-chain fan-out-debt continuations.
func TestReducerSupersededNullActiveClaimAndSweepLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #11703 null-active claim/sweep proof")
	}
	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)
	now := time.Date(2026, time.November, 28, 12, 0, 0, 0, time.UTC)

	seedNullActiveScope(t, ctx, db, "s-ret-plain", "", "g-ret-plain", "superseded", now)
	insertNullActiveWorkItem(t, ctx, db, "wi-ret-pending", "s-ret-plain", "g-ret-plain", "reducer", string(reducer.DomainSupplyChainImpact), "pending", `{}`, "", now)
	insertNullActiveWorkItem(t, ctx, db, "wi-ret-retrying", "s-ret-plain", "g-ret-plain", "reducer", string(reducer.DomainSupplyChainImpact), "retrying", `{}`, "transient_err", now)
	insertNullActiveWorkItem(t, ctx, db, "wi-ret-failed", "s-ret-plain", "g-ret-plain", "reducer", string(reducer.DomainSupplyChainImpact), "failed", `{}`, "prior_boom", now)
	insertNullActiveWorkItem(t, ctx, db, "wi-ret-dead", "s-ret-plain", "g-ret-plain", "reducer", string(reducer.DomainSupplyChainImpact), "dead_letter", `{}`, "prior_dead", now)
	insertNullActiveWorkItem(t, ctx, db, "wi-ret-claimed", "s-ret-plain", "g-ret-plain", "reducer", string(reducer.DomainSupplyChainImpact), "claimed", `{}`, "", now)
	insertNullActiveWorkItem(t, ctx, db, "wi-ret-proj", "s-ret-plain", "g-ret-plain", "projector", "source_local", "pending", `{}`, "", now)

	seedNullActiveScope(t, ctx, db, "s-ret-ready", "", "g-ret-ready", "superseded", now)
	insertNullActiveWorkItem(t, ctx, db, "wi-ret-readiness", "s-ret-ready", "g-ret-ready", "reducer", string(reducer.DomainAWSRelationshipMaterialization), "pending", `{"entity_key":"aws-unit-1"}`, "", now)

	seedNullActiveScope(t, ctx, db, "s-ret-debt", "", "g-ret-debt", "superseded", now)
	debtOwed := `{"` + supplychaincore.PayloadKeyFanOutOwed + `":"true"}`
	debtList := `{"` + supplychaincore.PayloadKeyFanOutRemaining + `":["scope:holder-1"]}`
	insertNullActiveWorkItem(t, ctx, db, "wi-ret-debt-owed", "s-ret-debt", "g-ret-debt", "reducer", string(reducer.DomainSupplyChainImpact), "pending", debtOwed, "", now)
	insertNullActiveWorkItem(t, ctx, db, "wi-ret-debt-list", "s-ret-debt", "g-ret-debt", "reducer", string(reducer.DomainSupplyChainImpact), "pending", debtList, "", now)

	seedNullActiveScope(t, ctx, db, "s-failed-null", "", "g-failed-null", "failed", now)
	insertNullActiveWorkItem(t, ctx, db, "wi-failed-gen-pending", "s-failed-null", "g-failed-null", "reducer", string(reducer.DomainSupplyChainImpact), "pending", `{}`, "", now)
	insertNullActiveWorkItem(t, ctx, db, "wi-failed-gen-failed", "s-failed-null", "g-failed-null", "reducer", string(reducer.DomainSupplyChainImpact), "failed", `{}`, "failed_scope_err", now)

	seedNullActiveScope(t, ctx, db, "s-pending-null", "", "g-pending-null", "pending", now)
	insertNullActiveWorkItem(t, ctx, db, "wi-pending-gen", "s-pending-null", "g-pending-null", "reducer", string(reducer.DomainSupplyChainImpact), "pending", `{}`, "", now)

	seedNullActiveScope(t, ctx, db, "s-dangling", "g-does-not-exist", "g-dangling-sup", "superseded", now)
	insertNullActiveWorkItem(t, ctx, db, "wi-dangling-ptr", "s-dangling", "g-dangling-sup", "reducer", string(reducer.DomainSupplyChainImpact), "pending", `{}`, "", now)

	queue := staleGateQueue(db, "owner-11703", now.Add(5*time.Minute))
	intents, err := queue.ClaimBatch(ctx, 32)
	if err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
	got := intentIDSet(intents)
	for _, wantClaimed := range []string{
		"wi-ret-debt-owed",
		"wi-ret-debt-list",
		"wi-failed-gen-pending",
		"wi-pending-gen",
		"wi-dangling-ptr",
	} {
		if !got[wantClaimed] {
			t.Errorf("ClaimBatch missed spared row %q; got=%v", wantClaimed, got)
		}
	}
	for _, wantFenced := range []string{"wi-ret-pending", "wi-ret-retrying", "wi-ret-readiness"} {
		if got[wantFenced] {
			t.Errorf("ClaimBatch handed out retired-scope row %q; got=%v", wantFenced, got)
		}
	}
	if _, claimed, err := queue.Claim(ctx); err != nil || claimed {
		t.Fatalf("single Claim after ClaimBatch = claimed %v err %v, want false (only retired rows remain)", claimed, err)
	}
	// Claim gate is read-only: fenced rows stay in their seeded status until swept.
	for id, want := range map[string]string{
		"wi-ret-pending":   "pending",
		"wi-ret-retrying":  "retrying",
		"wi-ret-readiness": "pending",
	} {
		if st := staleGateRowStatus(t, ctx, db, id); st != want {
			t.Fatalf("row %q status after claim = %q, want %q (read-only gate)", id, st, want)
		}
	}

	swept, err := queue.SupersedeStaleReducerGenerations(ctx, 100)
	if err != nil {
		t.Fatalf("SupersedeStaleReducerGenerations: %v", err)
	}
	if swept != 5 {
		t.Fatalf("SupersedeStaleReducerGenerations superseded %d rows, want 5", swept)
	}
	for id, want := range map[string]string{
		"wi-ret-pending":        "superseded",
		"wi-ret-retrying":       "superseded",
		"wi-ret-failed":         "superseded",
		"wi-ret-dead":           "superseded",
		"wi-ret-readiness":      "superseded",
		"wi-ret-claimed":        "claimed",
		"wi-ret-proj":           "pending",
		"wi-ret-debt-owed":      "claimed",
		"wi-ret-debt-list":      "claimed",
		"wi-failed-gen-pending": "claimed",
		"wi-failed-gen-failed":  "failed",
		"wi-pending-gen":        "claimed",
		"wi-dangling-ptr":       "claimed",
	} {
		if st := staleGateRowStatus(t, ctx, db, id); st != want {
			t.Errorf("row %q status after sweep = %q, want %q", id, st, want)
		}
	}
	// Verify failure_details on swept retired row records active_generation_id=null and prior_failure.
	var detailsRaw string
	if err := db.QueryRowContext(ctx, `SELECT failure_details FROM fact_work_items WHERE work_item_id = 'wi-ret-failed'`).Scan(&detailsRaw); err != nil {
		t.Fatalf("read failure_details: %v", err)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(detailsRaw), &details); err != nil {
		t.Fatalf("decode failure_details %q: %v", detailsRaw, err)
	}
	if val, present := details["active_generation_id"]; !present || val != nil {
		t.Fatalf("failure_details.active_generation_id = %#v (present=%v), want JSON null", val, present)
	}
	pf, ok := details["prior_failure"].(map[string]any)
	if !ok || pf["failure_class"] != "prior_boom" || pf["status"] != "failed" {
		t.Fatalf("failure_details.prior_failure = %#v, want status=failed failure_class=prior_boom", details["prior_failure"])
	}
	if again, err := queue.SupersedeStaleReducerGenerations(ctx, 100); err != nil || again != 0 {
		t.Fatalf("second sweep = %d err %v, want 0 (idempotent)", again, err)
	}
}

// TestReducerSupersededNullActiveEvalPlanQualAndConcurrencyLive proves
// EvalPlanQual recheck safety (concurrent claim or fan-out-debt update wins
// over the sweep) and concurrent SKIP LOCKED sweeps over retired scopes.
func TestReducerSupersededNullActiveEvalPlanQualAndConcurrencyLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #11703 concurrency proof")
	}
	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)
	now := time.Date(2026, time.November, 28, 12, 0, 0, 0, time.UTC)

	// Part 1: EvalPlanQual recheck when a concurrent tx mutates a retired row
	// (either claiming it or adding fanout_owed) before the blocking sweep locks it.
	for _, tc := range []struct {
		name      string
		scopeID   string
		genID     string
		workID    string
		updateSQL string
		wantSt    string
	}{
		{
			name:      "concurrent claim wins via EvalPlanQual",
			scopeID:   "s-epq-claim",
			genID:     "g-epq-claim",
			workID:    "wi-epq-claim",
			updateSQL: `UPDATE fact_work_items SET status = 'claimed', lease_owner = 'racer', claim_until = $2 WHERE work_item_id = $1`,
			wantSt:    "claimed",
		},
		{
			name:      "concurrent fanout debt write wins via EvalPlanQual",
			scopeID:   "s-epq-debt",
			genID:     "g-epq-debt",
			workID:    "wi-epq-debt",
			updateSQL: `UPDATE fact_work_items SET payload = payload || '{"fanout_owed":"true"}'::jsonb, updated_at = $2 WHERE work_item_id = $1`,
			wantSt:    "pending",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seedNullActiveScope(t, ctx, db, tc.scopeID, "", tc.genID, "superseded", now)
			insertNullActiveWorkItem(t, ctx, db, tc.workID, tc.scopeID, tc.genID, "reducer", string(reducer.DomainSupplyChainImpact), "pending", `{}`, "", now)

			tx1, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin tx1: %v", err)
			}
			defer func() { _ = tx1.Rollback() }()
			if _, err := tx1.ExecContext(ctx, tc.updateSQL, tc.workID, now.Add(time.Hour)); err != nil {
				t.Fatalf("tx1 update: %v", err)
			}

			db2 := openReducerFairnessClaimerDB(t, ctx, targetDSN)
			sweepDone := make(chan error, 1)
			go func() {
				blockingSweep := "WITH " + reducerClaimReadinessRequirementsCTE() + "," +
					supersedeInactiveReducerGenerationsCTE +
					"SELECT count(*) FROM superseded_stale_reducer_generations"
				var n int
				sweepDone <- db2.QueryRowContext(ctx, blockingSweep, now.Add(5*time.Minute), nil).Scan(&n)
			}()
			select {
			case err := <-sweepDone:
				t.Fatalf("blocking sweep finished before tx1 commit (did not contend on row lock): err=%v", err)
			case <-time.After(150 * time.Millisecond):
			}
			if err := tx1.Commit(); err != nil {
				t.Fatalf("tx1 commit: %v", err)
			}
			if err := <-sweepDone; err != nil {
				t.Fatalf("blocking sweep: %v", err)
			}
			if got := staleGateRowStatus(t, ctx, db, tc.workID); got != tc.wantSt {
				t.Fatalf("row %q status = %q, want %q after EvalPlanQual recheck", tc.workID, got, tc.wantSt)
			}
			// Clean up row so subsequent subtests stay isolated.
			mustExecGateArgs(t, ctx, db, `DELETE FROM fact_work_items WHERE work_item_id = $1`, tc.workID)
		})
	}

	// Part 2: 4 concurrent SKIP LOCKED sweepers over 12 retired NULL-active rows.
	seedNullActiveScope(t, ctx, db, "s-ret-conc", "", "g-ret-conc", "superseded", now)
	const rows = 12
	for i := 0; i < rows; i++ {
		insertNullActiveWorkItem(t, ctx, db, fmt.Sprintf("wi-ret-conc-%02d", i), "s-ret-conc", "g-ret-conc", "reducer", string(reducer.DomainSupplyChainImpact), "pending", `{}`, "", now)
	}
	var (
		mu    sync.Mutex
		total int
		wg    sync.WaitGroup
	)
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			sdb := openReducerFairnessClaimerDB(t, ctx, targetDSN)
			q := staleGateQueue(sdb, fmt.Sprintf("sw-%d", w), now.Add(5*time.Minute))
			for pass := 0; pass < 10; pass++ {
				n, err := q.SupersedeStaleReducerGenerations(ctx, 3)
				if err != nil {
					t.Errorf("sweeper %d: %v", w, err)
					return
				}
				mu.Lock()
				total += n
				mu.Unlock()
				if n == 0 {
					return
				}
			}
		}(w)
	}
	wg.Wait()
	if total != rows {
		t.Fatalf("concurrent sweeps superseded %d retired rows, want %d exactly once", total, rows)
	}
}

// TestActiveFactWorkItemsNullActiveSupersededParityLive proves both
// activeFactWorkItemsCTE (materialized) and activeFactWorkItemsPerRowCTE
// (per-row), plus QueueObserverStore.QueueDepths and readActiveWorkSummary,
// hide unleased reducer rows on retired scopes while keeping claimed,
// projector, failed-generation, pending-generation, and dangling-pointer rows
// visible.
func TestActiveFactWorkItemsNullActiveSupersededParityLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #11703 active_fact_work_items parity proof")
	}
	ctx := context.Background()
	conn := openStatusSemanticsSchema(ctx, t, dsn)
	resetHistoryCase(ctx, t, conn)

	historyScopes(ctx, t, conn,
		[2]string{"s-ret", ""},
		[2]string{"s-fail", ""},
		[2]string{"s-pend", ""},
		[2]string{"s-dang", "g-missing"},
	)
	historyGenerations(ctx, t, conn,
		[4]string{"g-ret", "s-ret", "2026-10-05 00:00:00+00", "superseded"},
		[4]string{"g-fail", "s-fail", "2026-10-05 00:00:00+00", "failed"},
		[4]string{"g-pend", "s-pend", "2026-10-05 00:00:00+00", "pending"},
		[4]string{"g-dang", "s-dang", "2026-10-05 00:00:00+00", "superseded"},
	)
	mk := func(id, scope, gen, stage, status string) historyWork {
		return historyWork{
			id: id, scope: scope, gen: gen, stage: stage, domain: "supply_chain_impact",
			status: status, created: historyCreated, updated: historyUpdated,
		}
	}
	historyWorkRows(ctx, t, conn,
		mk("w-ret-pending", "s-ret", "g-ret", "reducer", "pending"),
		mk("w-ret-retrying", "s-ret", "g-ret", "reducer", "retrying"),
		mk("w-ret-failed", "s-ret", "g-ret", "reducer", "failed"),
		mk("w-ret-dead", "s-ret", "g-ret", "reducer", "dead_letter"),
		mk("w-ret-claimed", "s-ret", "g-ret", "reducer", "claimed"),
		mk("w-ret-proj", "s-ret", "g-ret", "projector", "pending"),
		mk("w-fail-pending", "s-fail", "g-fail", "reducer", "pending"),
		mk("w-pend-pending", "s-pend", "g-pend", "reducer", "pending"),
		mk("w-dang-pending", "s-dang", "g-dang", "reducer", "pending"),
	)

	selectIDs := func(cte string) []string {
		return statusSemanticsRows(ctx, t, conn,
			"WITH "+cte+" SELECT work_item_id FROM active_fact_work_items ORDER BY work_item_id")
	}
	mat := selectIDs(activeFactWorkItemsCTE)
	perRow := selectIDs(activeFactWorkItemsPerRowCTE)
	if strings.Join(mat, ",") != strings.Join(perRow, ",") {
		t.Fatalf("active work forms disagree:\n materialized: %v\n per-row:      %v", mat, perRow)
	}
	wantVisible := []string{"w-dang-pending", "w-fail-pending", "w-pend-pending", "w-ret-claimed", "w-ret-proj"}
	if strings.Join(mat, ",") != strings.Join(wantVisible, ",") {
		t.Fatalf("visible active_fact_work_items = %v, want %v", mat, wantVisible)
	}

	depths, err := NewQueueObserverStore(sqlConnQueryer{conn: conn}).QueueDepths(ctx)
	if err != nil {
		t.Fatalf("QueueDepths: %v", err)
	}
	if got := depths["reducer"]["pending"]; got != 3 {
		t.Fatalf("QueueDepths()[reducer][pending] = %d, want 3 (failed/pending/dangling only, retired hidden)", got)
	}
	if got := depths["reducer"]["retrying"]; got != 0 {
		t.Fatalf("QueueDepths()[reducer][retrying] = %d, want 0 (retired retrying hidden)", got)
	}
	if got := depths["reducer"]["in_flight"]; got != 1 {
		t.Fatalf("QueueDepths()[reducer][in_flight] = %d, want 1 (retired claimed stays visible)", got)
	}
	if got := depths["projector"]["pending"]; got != 1 {
		t.Fatalf("QueueDepths()[projector][pending] = %d, want 1 (projector stays visible)", got)
	}
}
