// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10828 disposable PostgreSQL proof that the NVD baseline per-year plan row commits or rolls back with the work items and slot progress of one guarded admission, is written only for a chunk the insert accepted (never one the terminal-run skip, the open-target guard, or a primary-key conflict dropped), is stamped from the database clock after the planning-lock wait, and is not written by the plain or slot-progress entry points; isolated bootstrap schema with migration 312
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const planLiveOSVScope = "vuln-intel://osv/npm/left-pad"

// planLiveDB is the store's database handle for the #10828 proofs: the real
// SQLDB, a count of the transactions begun (a retry shows up as one more), and a
// hook that wraps each transaction a test opens.
type planLiveDB struct {
	SQLDB
	mu     sync.Mutex
	begins int
	wrap   func(db.Transaction) db.Transaction
}

func (p *planLiveDB) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := p.SQLDB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.begins++
	wrap := p.wrap
	p.mu.Unlock()
	if wrap != nil {
		return wrap(tx), nil
	}
	return tx, nil
}

func (p *planLiveDB) beginCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.begins
}

func (p *planLiveDB) setWrap(wrap func(db.Transaction) db.Transaction) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.wrap = wrap
}

// planLiveAdmission builds one admission for an instance: the run, one baseline
// chunk per year whose scope id carries start, and the plans the coordinator
// derives for them. Work item ids carry the run, so two runs of one instance
// never collide unless a test makes them.
func planLiveAdmission(suffix, instanceID string, start time.Time, years ...int) (workflow.Run, []workflow.WorkItem, []workflow.NVDBaselinePlan) {
	now := time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              fmt.Sprintf("vulnerability_intelligence:%s:schedule:%s", instanceID, suffix),
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorVulnerabilityIntelligence),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	items := make([]workflow.WorkItem, 0, len(years))
	plans := make([]workflow.NVDBaselinePlan, 0, len(years))
	for _, year := range years {
		item := baselineChunkItem(run.RunID, instanceID, year, start, now)
		item.WorkItemID = fmt.Sprintf("%s:%d", run.RunID, year)
		items = append(items, item)
		plans = append(plans, workflow.NVDBaselinePlan{BaselineRunID: baselineTestRunID, Year: year, ScopeID: item.ScopeID})
	}
	return run, items, plans
}

// planLiveOSVItem is an ordinary, non-baseline target for the same run.
func planLiveOSVItem(run workflow.Run, instanceID string) workflow.WorkItem {
	item := baselineChunkItem(run.RunID, instanceID, 2026, baselineTestStart, run.CreatedAt)
	item.WorkItemID = run.RunID + ":osv"
	item.ScopeID = planLiveOSVScope
	item.AcceptanceUnitID = planLiveOSVScope
	return item
}

type planLiveRow struct {
	scopeID   string
	plannedAt time.Time
}

// planLiveRows reads the plan rows of the proof's baseline run, keyed by year.
func planLiveRows(ctx context.Context, t *testing.T, database *sql.DB) map[int]planLiveRow {
	t.Helper()
	rows, err := database.QueryContext(ctx, `
SELECT year, last_planned_scope_id, last_planned_at
FROM vulnerability_nvd_baseline_plans
WHERE baseline_run_id = $1
ORDER BY year`, baselineTestRunID)
	if err != nil {
		t.Fatalf("read plan rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := map[int]planLiveRow{}
	for rows.Next() {
		var year int
		var row planLiveRow
		if err := rows.Scan(&year, &row.scopeID, &row.plannedAt); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		got[year] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan rows: %v", err)
	}
	return got
}

// planLiveCount counts the rows of one fixed table name.
func planLiveCount(ctx context.Context, t *testing.T, database *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

// planLiveClock reads the database clock, the clock the plan row is stamped
// from.
func planLiveClock(ctx context.Context, t *testing.T, database *sql.DB) time.Time {
	t.Helper()
	var now time.Time
	if err := database.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatalf("read database clock: %v", err)
	}
	return now
}

// planLiveReset empties every table an admission writes, so each subtest starts
// from nothing.
func planLiveReset(ctx context.Context, t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
TRUNCATE vulnerability_nvd_baseline_plans, owned_target_slot_progress, workflow_work_items, workflow_runs CASCADE`); err != nil {
		t.Fatalf("reset admission tables: %v", err)
	}
}

// planLiveTrigger installs a BEFORE INSERT OR UPDATE row trigger on the plan
// table whose body runs for every row the upsert writes, and removes it when the
// subtest ends. body is PL/pgSQL.
func planLiveTrigger(ctx context.Context, t *testing.T, database *sql.DB, name, body string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, fmt.Sprintf(`
CREATE OR REPLACE FUNCTION %[1]s() RETURNS trigger LANGUAGE plpgsql AS $fn$
BEGIN
    %[2]s
    RETURN NEW;
END
$fn$;
CREATE TRIGGER %[1]s BEFORE INSERT OR UPDATE ON vulnerability_nvd_baseline_plans
FOR EACH ROW EXECUTE FUNCTION %[1]s();`, name, body)); err != nil {
		t.Fatalf("install trigger %s: %v", name, err)
	}
	t.Cleanup(func() { planLiveDropTrigger(context.Background(), t, database, name) })
}

func planLiveDropTrigger(ctx context.Context, t *testing.T, database *sql.DB, name string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, fmt.Sprintf(
		`DROP TRIGGER IF EXISTS %[1]s ON vulnerability_nvd_baseline_plans; DROP FUNCTION IF EXISTS %[1]s();`, name)); err != nil {
		t.Fatalf("drop trigger %s: %v", name, err)
	}
}

// planLiveSeedTerminalRun inserts a finished run, and one completed work item in
// it when item is set.
func planLiveSeedTerminalRun(ctx context.Context, t *testing.T, database *sql.DB, runID string, item *workflow.WorkItem) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_runs (run_id, trigger_kind, status, requested_scope_set, requested_collector, created_at, updated_at)
VALUES ($1, 'schedule', 'complete', '[]'::jsonb, 'vulnerability_intelligence', now(), now())`, runID); err != nil {
		t.Fatalf("seed terminal run: %v", err)
	}
	if item == nil {
		return
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind, collector_instance_id, source_system,
    scope_id, acceptance_unit_id, source_run_id, generation_id, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $3, $5, $5, $1, $1, 'completed', now(), now())`,
		item.WorkItemID, runID, string(item.CollectorKind), item.CollectorInstanceID, item.ScopeID); err != nil {
		t.Fatalf("seed terminal work item: %v", err)
	}
}

// TestWorkflowControlBaselinePlansLive proves, on one real database, what the
// hermetic tests can only record: the plan row and the work items of an
// admission are one transaction, and a plan row exists only for a chunk the
// insert accepted.
func TestWorkflowControlBaselinePlansLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "baseline_plans_10828", "ESHU_POSTGRES_DSN is not set")
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	handle := &planLiveDB{SQLDB: SQLDB{DB: database}}
	store := NewWorkflowControlStore(handle)

	t.Run("an admission commits its plan rows with its work items", func(t *testing.T) {
		planLiveReset(ctx, t, database)
		run, items, plans := planLiveAdmission("commit", "vuln-a", baselineTestStart, 2026, 2025)
		items = append(items, planLiveOSVItem(run, "vuln-a"))

		before := planLiveClock(ctx, t, database)
		admission, result, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(ctx, run, items, plans, nil)
		after := planLiveClock(ctx, t, database)
		if err != nil {
			t.Fatalf("CreateRunWithWorkItemsRecordingBaselinePlans() error = %v", err)
		}
		if admission.EligibleTargets != 3 || admission.InsertedWorkItems != 3 || admission.BaselinePlansRecorded != 2 {
			t.Fatalf("admission = %+v, want 3 eligible, 3 inserted, 2 plan rows recorded", admission)
		}
		if result != (workflow.SlotAdvanceResult{}) {
			t.Fatalf("slot result = %+v, want none for a nil advance", result)
		}
		rows := planLiveRows(ctx, t, database)
		if len(rows) != 2 {
			t.Fatalf("plan rows = %+v, want exactly the two baseline years and none for the OSV target", rows)
		}
		for i, year := range []int{2026, 2025} {
			row, ok := rows[year]
			if !ok || row.scopeID != items[i].ScopeID {
				t.Fatalf("plan row %d = %+v, want the chunk scope %q", year, row, items[i].ScopeID)
			}
			if row.plannedAt.Before(before) || row.plannedAt.After(after) {
				t.Fatalf("plan row %d stamped %v, want a database-clock time in [%v, %v]", year, row.plannedAt, before, after)
			}
		}
		if got := planLiveCount(ctx, t, database, "workflow_work_items"); got != 3 {
			t.Fatalf("work items = %d, want all 3 committed with the plan rows", got)
		}
	})

	t.Run("a refused plan write rolls the whole admission back, slot move included", func(t *testing.T) {
		planLiveReset(ctx, t, database)
		planLiveTrigger(ctx, t, database, "refuse_plan_write_10828", `RAISE EXCEPTION 'plan write refused for the proof';`)
		run, items, plans := planLiveAdmission("refused", progLiveInstance, baselineTestStart, 2026, 2025)
		advance := progLiveAdvanceFor([]int{3, 7}, 1)

		_, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(ctx, run, items, plans, &advance)
		if err == nil || !strings.Contains(err.Error(), "plan write refused for the proof") {
			t.Fatalf("error = %v, want the refused plan write", err)
		}
		for _, table := range []string{"workflow_runs", "workflow_work_items", "vulnerability_nvd_baseline_plans", "owned_target_slot_progress"} {
			if got := planLiveCount(ctx, t, database, table); got != 0 {
				t.Fatalf("%s holds %d rows after the refused plan write, want 0 (one transaction)", table, got)
			}
		}

		// The same admission against a healthy table commits all four effects,
		// so the rollback above was the plan write's doing and not the fixture's.
		planLiveDropTrigger(ctx, t, database, "refuse_plan_write_10828")
		admission, result, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(ctx, run, items, plans, &advance)
		if err != nil {
			t.Fatalf("control admission error = %v", err)
		}
		if admission.InsertedWorkItems != 2 || admission.BaselinePlansRecorded != 2 || result.Moved != 2 {
			t.Fatalf("control admission = %+v slots = %+v, want 2 items, 2 plan rows, 2 slots moved", admission, result)
		}
		for table, want := range map[string]int{"workflow_runs": 1, "workflow_work_items": 2, "vulnerability_nvd_baseline_plans": 2, "owned_target_slot_progress": 2} {
			if got := planLiveCount(ctx, t, database, table); got != want {
				t.Fatalf("%s holds %d rows after the control admission, want %d", table, got, want)
			}
		}
	})

	t.Run("a failed commit leaves no plan row behind", func(t *testing.T) {
		planLiveReset(ctx, t, database)
		handle.setWrap(func(tx db.Transaction) db.Transaction { return failCommitTx{tx} })
		t.Cleanup(func() { handle.setWrap(nil) })
		run, items, plans := planLiveAdmission("commit-fails", "vuln-a", baselineTestStart, 2026)

		_, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(ctx, run, items, plans, nil)
		if err == nil || !strings.Contains(err.Error(), "commit failed") {
			t.Fatalf("error = %v, want the commit failure", err)
		}
		// A plan upsert issued outside the admission transaction would have
		// committed on its own connection and survive this rollback.
		if got := planLiveCount(ctx, t, database, "vulnerability_nvd_baseline_plans"); got != 0 {
			t.Fatalf("plan rows = %d after a failed commit, want 0", got)
		}
		if got := planLiveCount(ctx, t, database, "workflow_work_items"); got != 0 {
			t.Fatalf("work items = %d after a failed commit, want 0", got)
		}
	})

	t.Run("the open-target guard drops a chunk and its plan row", func(t *testing.T) {
		planLiveReset(ctx, t, database)
		firstRun, firstItems, firstPlans := planLiveAdmission("first", "vuln-a", baselineTestStart, 2026)
		if _, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(ctx, firstRun, firstItems, firstPlans, nil); err != nil {
			t.Fatalf("first admission error = %v", err)
		}
		original := planLiveRows(ctx, t, database)[2026]

		// The same 2026 target is still open under the first run, so the guard
		// drops it; the 2025 chunk is new and lands.
		secondRun, secondItems, secondPlans := planLiveAdmission("second", "vuln-a", baselineTestStart, 2026, 2025)
		admission, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(ctx, secondRun, secondItems, secondPlans, nil)
		if err != nil {
			t.Fatalf("second admission error = %v", err)
		}
		if admission.EligibleTargets != 1 || admission.InsertedWorkItems != 1 || admission.BaselinePlansRecorded != 1 {
			t.Fatalf("admission = %+v, want 1 eligible, 1 inserted, 1 plan row (the guard dropped 2026)", admission)
		}
		rows := planLiveRows(ctx, t, database)
		if rows[2026] != original {
			t.Fatalf("2026 plan row = %+v, want it untouched at %+v: a guard-dropped chunk wrote a row", rows[2026], original)
		}
		if rows[2025].scopeID != secondItems[1].ScopeID {
			t.Fatalf("2025 plan row = %+v, want the admitted chunk %q", rows[2025], secondItems[1].ScopeID)
		}

		// Every chunk dropped by the guard: nothing is inserted and no row moves.
		thirdRun, thirdItems, thirdPlans := planLiveAdmission("third", "vuln-a", baselineTestStart, 2026, 2025)
		admission, _, err = store.CreateRunWithWorkItemsRecordingBaselinePlans(ctx, thirdRun, thirdItems, thirdPlans, nil)
		if err != nil {
			t.Fatalf("third admission error = %v", err)
		}
		if admission.EligibleTargets != 0 || admission.BaselinePlansRecorded != 0 {
			t.Fatalf("admission = %+v, want every chunk dropped by the guard and no plan row", admission)
		}
		if after := planLiveRows(ctx, t, database); after[2026] != original || after[2025] != rows[2025] {
			t.Fatalf("plan rows = %+v, want both untouched by a fully dropped admission", after)
		}
	})

	t.Run("a primary-key conflict drops a chunk the guard admitted, and its plan row", func(t *testing.T) {
		planLiveReset(ctx, t, database)
		prior := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
		if _, err := database.ExecContext(ctx, `
INSERT INTO vulnerability_nvd_baseline_plans (baseline_run_id, year, last_planned_scope_id, last_planned_at)
VALUES ($1, 2026, 'prior-scope', $2)`, baselineTestRunID, prior); err != nil {
			t.Fatalf("seed prior plan row: %v", err)
		}
		run, items, plans := planLiveAdmission("collide", "vuln-a", baselineTestStart, 2026, 2025)
		// A finished run already owns this work item id. Its run is terminal, so
		// the open-target guard admits the chunk, and the insert then drops it on
		// the work_item_id primary key.
		planLiveSeedTerminalRun(ctx, t, database, "vulnerability_intelligence:vuln-a:schedule:finished", &items[0])

		admission, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(ctx, run, items, plans, nil)
		if err != nil {
			t.Fatalf("CreateRunWithWorkItemsRecordingBaselinePlans() error = %v", err)
		}
		if admission.EligibleTargets != 2 || admission.InsertedWorkItems != 1 || admission.BaselinePlansRecorded != 1 {
			t.Fatalf("admission = %+v, want 2 eligible, 1 inserted, 1 plan row (the insert dropped 2026)", admission)
		}
		rows := planLiveRows(ctx, t, database)
		if rows[2026].scopeID != "prior-scope" || !rows[2026].plannedAt.Equal(prior) {
			t.Fatalf("2026 plan row = %+v, want it untouched: an insert-dropped chunk wrote a row", rows[2026])
		}
		if rows[2025].scopeID != items[1].ScopeID {
			t.Fatalf("2025 plan row = %+v, want the inserted chunk %q", rows[2025], items[1].ScopeID)
		}
	})

	t.Run("a terminal-run skip writes no plan row", func(t *testing.T) {
		planLiveReset(ctx, t, database)
		run, items, plans := planLiveAdmission("done", "vuln-a", baselineTestStart, 2026)
		planLiveSeedTerminalRun(ctx, t, database, run.RunID, nil)

		admission, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(ctx, run, items, plans, nil)
		if err != nil {
			t.Fatalf("CreateRunWithWorkItemsRecordingBaselinePlans() error = %v", err)
		}
		if admission.EligibleTargets != 0 || admission.InsertedWorkItems != 0 || admission.BaselinePlansRecorded != 0 {
			t.Fatalf("admission = %+v, want the terminal-run skip to admit nothing", admission)
		}
		if rows := planLiveRows(ctx, t, database); len(rows) != 0 {
			t.Fatalf("plan rows = %+v, want none for a skipped terminal run", rows)
		}
		if got := planLiveCount(ctx, t, database, "workflow_work_items"); got != 0 {
			t.Fatalf("work items = %d, want 0", got)
		}
	})

	t.Run("the plain and slot-progress entry points write no plan row", func(t *testing.T) {
		planLiveReset(ctx, t, database)
		plainRun, plainItems, _ := planLiveAdmission("plain", "vuln-a", baselineTestStart, 2026)
		if _, err := store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, plainRun, plainItems); err != nil {
			t.Fatalf("plain admission error = %v", err)
		}
		slotRun, slotItems, _ := planLiveAdmission("slot", progLiveInstance, baselineTestStart, 2025)
		advance := progLiveAdvanceFor([]int{1}, 1)
		if _, _, err := store.CreateRunWithWorkItemsAdvancingSlotProgress(ctx, slotRun, slotItems, advance); err != nil {
			t.Fatalf("slot-progress admission error = %v", err)
		}
		if got := planLiveCount(ctx, t, database, "workflow_work_items"); got != 2 {
			t.Fatalf("work items = %d, want both chunks admitted", got)
		}
		if got := planLiveCount(ctx, t, database, "vulnerability_nvd_baseline_plans"); got != 0 {
			t.Fatalf("plan rows = %d, want none: only the plan-carrying entry point records them", got)
		}
	})

	t.Run("the plan row is stamped from the database clock after the planning-lock wait", func(t *testing.T) {
		planLiveReset(ctx, t, database)
		run, items, plans := planLiveAdmission("clock", "vuln-a", baselineTestStart, 2026)

		// Hold the instance's planning lock from another session so the
		// admission's transaction begins, then waits. now() would stamp the row
		// with that earlier transaction start.
		conn, err := database.Conn(ctx)
		if err != nil {
			t.Fatalf("hold connection: %v", err)
		}
		defer func() { _ = conn.Close() }()
		key := workflowPlanningLockKey(items[0])
		if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, key); err != nil {
			t.Fatalf("hold planning lock: %v", err)
		}
		done := make(chan error, 1)
		go func() {
			_, _, admitErr := store.CreateRunWithWorkItemsRecordingBaselinePlans(ctx, run, items, plans, nil)
			done <- admitErr
		}()
		select {
		case err := <-done:
			t.Fatalf("the admission finished while the planning lock was held (error = %v)", err)
		case <-time.After(500 * time.Millisecond):
		}
		var released time.Time
		if err := conn.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&released); err != nil {
			t.Fatalf("read release time: %v", err)
		}
		if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, key); err != nil {
			t.Fatalf("release planning lock: %v", err)
		}
		if err := <-done; err != nil {
			t.Fatalf("admission error = %v", err)
		}
		row := planLiveRows(ctx, t, database)[2026]
		if row.plannedAt.Before(released) {
			t.Fatalf("plan row stamped %v, before the planning lock was released at %v: the stamp is the transaction start, not the write", row.plannedAt, released)
		}
	})
}
