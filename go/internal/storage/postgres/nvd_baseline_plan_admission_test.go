// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/vulnsource"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Hermetic proofs for the NVD baseline plan write that rides the guarded
// admission transaction (eshu-hq/eshu#10828). They record the statement order
// the admission issues and the arguments the plan upsert carries, so CI fails
// when the write leaves the transaction, runs on a path that inserted nothing,
// or covers a chunk the insert dropped. The live companion
// (nvd_baseline_plan_admission_live_test.go) proves the same properties on
// a real database.

const baselineTestRunID = "baseline-2026-11"

var baselineTestStart = time.UnixMilli(1_700_000_000_000).UTC()

// baselineChunkItem builds the work item the vulnerability coordinator plans
// for one baseline year, with the scope id the real mint writes.
func baselineChunkItem(runID, instanceID string, year int, start time.Time, now time.Time) workflow.WorkItem {
	scopeID := vulnsource.NVDBaselineScopeID(baselineTestRunID, year, start, "")
	generationID := fmt.Sprintf("vulnerability_intelligence:gen-%d-%d", year, start.UnixMilli())
	return workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("vulnerability_intelligence:%s:%s", instanceID, generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorVulnerabilityIntelligence,
		CollectorInstanceID: instanceID,
		SourceSystem:        string(scope.CollectorVulnerabilityIntelligence),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

// baselineAdmissionFixture returns the run, one work item per year, and the
// plans the coordinator derives for those baseline items.
func baselineAdmissionFixture(years ...int) (workflow.Run, []workflow.WorkItem, []workflow.NVDBaselinePlan) {
	now := time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "vulnerability_intelligence:vuln-a:schedule:continuous-20261117T090000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorVulnerabilityIntelligence),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	var items []workflow.WorkItem
	var plans []workflow.NVDBaselinePlan
	for _, year := range years {
		item := baselineChunkItem(run.RunID, "vuln-a", year, baselineTestStart, now)
		items = append(items, item)
		plans = append(plans, workflow.NVDBaselinePlan{BaselineRunID: baselineTestRunID, Year: year, ScopeID: item.ScopeID})
	}
	return run, items, plans
}

// planWriteFault wraps the ordered recorder so the plan upsert can fail or
// report an unexpected affected-row count while every other statement behaves.
type planWriteFault struct {
	*orderedStatementRecorder
	err      error
	affected int64
}

func (f *planWriteFault) Begin(context.Context) (db.Transaction, error) { return f, nil }

func (f *planWriteFault) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := f.orderedStatementRecorder.ExecContext(ctx, query, args...)
	if !strings.Contains(query, "vulnerability_nvd_baseline_plans") {
		return result, err
	}
	if f.err != nil {
		return nil, f.err
	}
	return fakeResultWithRowsAffected{rowsAffected: f.affected}, nil
}

// planUpsertArgs returns the three parallel arrays of the recorded plan upsert,
// or fails when the admission issued none or more than one.
func planUpsertArgs(t *testing.T, recorder *orderedStatementRecorder) (runs []string, years []int32, scopes []string) {
	t.Helper()
	args := recorder.argsOfStatements("INSERT INTO vulnerability_nvd_baseline_plans")
	if len(args) != 3 {
		t.Fatalf("plan upsert argument count = %d, want one statement with 3 parallel arrays", len(args))
	}
	var ok bool
	if runs, ok = args[0].([]string); !ok {
		t.Fatalf("plan upsert run argument = %T, want []string", args[0])
	}
	if years, ok = args[1].([]int32); !ok {
		t.Fatalf("plan upsert year argument = %T, want []int32", args[1])
	}
	if scopes, ok = args[2].([]string); !ok {
		t.Fatalf("plan upsert scope argument = %T, want []string", args[2])
	}
	return runs, years, scopes
}

// TestWorkflowControlStoreRecordsBaselinePlansInsideTheAdmission pins where the
// write sits: after the work-item insert (it names only inserted chunks), under
// the advisory lock admission already takes, before commit, and with no second
// lock. The lock is taken once because the plan row shares the work item's
// collector instance.
func TestWorkflowControlStoreRecordsBaselinePlansInsideTheAdmission(t *testing.T) {
	t.Parallel()

	run, items, plans := baselineAdmissionFixture(2026, 2025)
	recorder := &orderedStatementRecorder{
		rows: []queueFakeRows{
			{rows: [][]any{{false}}},  // terminal-run check
			{rows: [][]any{{0}, {1}}}, // both chunks eligible
			{rows: [][]any{{items[0].ScopeID}, {items[1].ScopeID}}},
		},
		execRows: []int64{1, 1, 2}, // lock, run insert, plan upsert
	}
	store := NewWorkflowControlStore(recorder)

	admission, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(context.Background(), run, items, plans, nil)
	if err != nil {
		t.Fatalf("CreateRunWithWorkItemsRecordingBaselinePlans() error = %v", err)
	}
	if admission.BaselinePlansRecorded != 2 || admission.InsertedWorkItems != 2 {
		t.Fatalf("admission = %+v, want 2 inserted work items and 2 recorded plans", admission)
	}
	lockAt := recorder.indexOfStatement("pg_advisory_xact_lock")
	insertAt := recorder.indexOfStatement("INSERT INTO workflow_work_items")
	planAt := recorder.indexOfStatement("INSERT INTO vulnerability_nvd_baseline_plans")
	if lockAt < 0 || insertAt < 0 || planAt < 0 {
		t.Fatalf("statement positions lock=%d insert=%d plan=%d, want all three issued", lockAt, insertAt, planAt)
	}
	if lockAt >= insertAt || insertAt >= planAt {
		t.Fatalf("order lock=%d insert=%d plan=%d, want advisory lock, work-item insert, plan upsert", lockAt, insertAt, planAt)
	}
	if got := len(recorder.argsOfStatements("pg_advisory_xact_lock")); got != 1 {
		t.Fatalf("advisory lock statements = %d, want the one the admission already takes and no new lock", got)
	}
	if !recorder.didCommit() || recorder.didRollBack() {
		t.Fatalf("committed = %v rolled back = %v, want the plan row to commit with its work item", recorder.didCommit(), recorder.didRollBack())
	}
	runs, years, scopes := planUpsertArgs(t, recorder)
	if len(runs) != 2 || runs[0] != baselineTestRunID || years[0] != 2025 || years[1] != 2026 {
		t.Fatalf("plan upsert rows = %v %v, want year-ascending rows for the run", runs, years)
	}
	if scopes[0] != items[1].ScopeID || scopes[1] != items[0].ScopeID {
		t.Fatalf("plan upsert scopes = %v, want each year to carry its own inserted chunk", scopes)
	}
}

// TestWorkflowControlStoreWritesPlansOnlyForInsertedChunks proves the plan list
// is cut to the scopes the insert accepted. Both chunks cleared the guard but
// the database accepted one (a primary-key or partial-unique conflict), so the
// dropped chunk must write no plan row: a plan row without its work item would
// read as an in-flight chunk that does not exist.
func TestWorkflowControlStoreWritesPlansOnlyForInsertedChunks(t *testing.T) {
	t.Parallel()

	run, items, plans := baselineAdmissionFixture(2026, 2025)
	recorder := &orderedStatementRecorder{
		rows: []queueFakeRows{
			{rows: [][]any{{false}}},
			{rows: [][]any{{0}, {1}}},
			{rows: [][]any{{items[1].ScopeID}}}, // only 2025 landed
		},
		execRows: []int64{1, 1, 1},
	}
	store := NewWorkflowControlStore(recorder)

	admission, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(context.Background(), run, items, plans, nil)
	if err != nil {
		t.Fatalf("CreateRunWithWorkItemsRecordingBaselinePlans() error = %v", err)
	}
	if admission.BaselinePlansRecorded != 1 || admission.InsertedWorkItems != 1 || admission.EligibleTargets != 2 {
		t.Fatalf("admission = %+v, want 1 recorded plan beside 1 inserted of 2 eligible", admission)
	}
	_, years, scopes := planUpsertArgs(t, recorder)
	if len(years) != 1 || years[0] != 2025 || scopes[0] != items[1].ScopeID {
		t.Fatalf("plan upsert = years %v scopes %v, want only the inserted 2025 chunk", years, scopes)
	}
}

// TestWorkflowControlStoreWritesNoPlanWhenNothingWasInserted covers the three
// commit paths that insert no work item: a terminal run replay, an admission the
// open-target guard emptied, and an insert that dropped every row. None may
// touch the plan table, and each still commits what it always committed.
func TestWorkflowControlStoreWritesNoPlanWhenNothingWasInserted(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		rows         []queueFakeRows
		wantCommit   bool
		wantEligible int
	}{
		"terminal run replay": {
			rows:       []queueFakeRows{{rows: [][]any{{true}}}},
			wantCommit: true,
		},
		"guard dropped every chunk": {
			rows:       []queueFakeRows{{rows: [][]any{{false}}}, {rows: nil}},
			wantCommit: true,
		},
		"insert dropped every chunk": {
			rows:         []queueFakeRows{{rows: [][]any{{false}}}, {rows: [][]any{{0}, {1}}}, {rows: nil}},
			wantCommit:   true,
			wantEligible: 2,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			run, items, plans := baselineAdmissionFixture(2026, 2025)
			recorder := &orderedStatementRecorder{rows: tc.rows, execRows: []int64{1, 1, 1}}
			store := NewWorkflowControlStore(recorder)

			admission, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(context.Background(), run, items, plans, nil)
			if err != nil {
				t.Fatalf("CreateRunWithWorkItemsRecordingBaselinePlans() error = %v", err)
			}
			if recorder.indexOfStatement("vulnerability_nvd_baseline_plans") >= 0 {
				t.Fatal("the admission wrote a plan row although it inserted no work item")
			}
			if admission.BaselinePlansRecorded != 0 || admission.EligibleTargets != tc.wantEligible {
				t.Fatalf("admission = %+v, want 0 recorded plans and %d eligible targets", admission, tc.wantEligible)
			}
			if recorder.didCommit() != tc.wantCommit {
				t.Fatalf("committed = %v, want %v", recorder.didCommit(), tc.wantCommit)
			}
		})
	}
}

// TestWorkflowControlStoreRollsBackWhenThePlanWriteFails pins the atomic half
// of the contract: the plan row and its work item commit or roll back together.
// A failing plan statement, and a statement that wrote a different number of
// rows than it was handed, both abort the admission with the work items still
// uncommitted.
func TestWorkflowControlStoreRollsBackWhenThePlanWriteFails(t *testing.T) {
	t.Parallel()

	cases := map[string]*planWriteFault{
		"statement error":     {err: errors.New("permission denied for table vulnerability_nvd_baseline_plans")},
		"short row count":     {affected: 0},
		"surplus row count":   {affected: 3},
		"missing table error": {err: errors.New(`relation "vulnerability_nvd_baseline_plans" does not exist`)},
	}
	for name, fault := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			run, items, plans := baselineAdmissionFixture(2026, 2025)
			fault.orderedStatementRecorder = &orderedStatementRecorder{
				rows: []queueFakeRows{
					{rows: [][]any{{false}}},
					{rows: [][]any{{0}, {1}}},
					{rows: [][]any{{items[0].ScopeID}, {items[1].ScopeID}}},
				},
				execRows: []int64{1, 1},
			}
			store := NewWorkflowControlStore(fault)

			admission, _, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(context.Background(), run, items, plans, nil)
			if err == nil {
				t.Fatalf("error = nil (admission = %+v), want the plan write failure to abort the admission", admission)
			}
			if fault.didCommit() {
				t.Fatal("the admission committed work items whose plan rows did not land")
			}
			if !fault.didRollBack() {
				t.Fatal("the admission did not roll back after the plan write failed")
			}
			if admission.InsertedWorkItems != 0 || admission.BaselinePlansRecorded != 0 {
				t.Fatalf("admission = %+v, want a zero admission alongside the error", admission)
			}
		})
	}
}

// TestWorkflowControlStoreSlotAdvanceAndBaselinePlanShareOneTransaction covers
// an owned-package pick and a baseline chunk planned in one tick: both writes
// ride the one admission, the slot move first and the plan row last, so the
// contended plan row is held for the shortest time before commit.
func TestWorkflowControlStoreSlotAdvanceAndBaselinePlanShareOneTransaction(t *testing.T) {
	t.Parallel()

	run, items, plans := baselineAdmissionFixture(2026)
	advance := &workflow.OwnedTargetSlotAdvance{
		CollectorKind:       scope.CollectorVulnerabilityIntelligence,
		CollectorInstanceID: "vuln-a",
		DerivationKind:      workflow.SlotDerivationKindVulnerability,
		WindowStartUnix:     1_700_000_000,
		PlannedAt:           time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC),
		Slots:               []int{3},
	}
	recorder := &orderedStatementRecorder{
		rows: []queueFakeRows{
			{rows: [][]any{{false}}},
			{rows: [][]any{{0}}},
			{rows: [][]any{{items[0].ScopeID}}},
		},
		execRows: []int64{1, 1, 1, 1}, // lock, run insert, slot advance, plan upsert
	}
	store := NewWorkflowControlStore(recorder)

	admission, moved, err := store.CreateRunWithWorkItemsRecordingBaselinePlans(context.Background(), run, items, plans, advance)
	if err != nil {
		t.Fatalf("CreateRunWithWorkItemsRecordingBaselinePlans() error = %v", err)
	}
	if admission.BaselinePlansRecorded != 1 || moved.Moved != 1 {
		t.Fatalf("admission = %+v moved = %+v, want one plan and one slot moved", admission, moved)
	}
	slotAt := recorder.indexOfStatement("INSERT INTO owned_target_slot_progress")
	planAt := recorder.indexOfStatement("INSERT INTO vulnerability_nvd_baseline_plans")
	if slotAt < 0 || planAt < 0 || slotAt > planAt {
		t.Fatalf("slot advance at %d, plan upsert at %d, want both issued with the plan row last", slotAt, planAt)
	}
	if !recorder.didCommit() {
		t.Fatal("the combined admission did not commit")
	}
}

// TestWorkflowControlStorePlainEntryPointsWriteNoPlanRows pins who owns the
// plan list: only the plan-carrying entry point records rows. The plain guarded
// admission and the slot-progress admission carry no plans, so a baseline item
// admitted through them leaves no plan row. The coordinator derives the list
// for every admission it routes (see its own tests), so this is the contract the
// coordinator relies on and not a gap.
func TestWorkflowControlStorePlainEntryPointsWriteNoPlanRows(t *testing.T) {
	t.Parallel()

	for name, call := range map[string]func(*WorkflowControlStore, workflow.Run, []workflow.WorkItem) error{
		"plain guarded admission": func(s *WorkflowControlStore, run workflow.Run, items []workflow.WorkItem) error {
			_, err := s.CreateRunWithWorkItemsIfNoOpenTargets(context.Background(), run, items)
			return err
		},
		"slot-progress admission": func(s *WorkflowControlStore, run workflow.Run, items []workflow.WorkItem) error {
			_, _, err := s.CreateRunWithWorkItemsAdvancingSlotProgress(context.Background(), run, items, workflow.OwnedTargetSlotAdvance{
				CollectorKind:       scope.CollectorVulnerabilityIntelligence,
				CollectorInstanceID: "vuln-a",
				DerivationKind:      workflow.SlotDerivationKindVulnerability,
				WindowStartUnix:     1,
				PlannedAt:           time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC),
				Slots:               []int{1},
			})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			run, items, _ := baselineAdmissionFixture(2026)
			recorder := &orderedStatementRecorder{
				rows: []queueFakeRows{
					{rows: [][]any{{false}}},
					{rows: [][]any{{0}}},
					{rows: [][]any{{items[0].ScopeID}}},
				},
				execRows: []int64{1, 1, 1},
			}
			if err := call(NewWorkflowControlStore(recorder), run, items); err != nil {
				t.Fatalf("admission error = %v", err)
			}
			if recorder.indexOfStatement("vulnerability_nvd_baseline_plans") >= 0 {
				t.Fatal("an entry point that carries no plans wrote a plan row")
			}
		})
	}
}
