// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/vulnsource"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// CreateRunWithWorkItemsRecordingBaselinePlans is
// [WorkflowControlStore.CreateRunWithWorkItemsIfNoOpenTargets] plus the NVD
// baseline per-year plan rows (eshu-hq/eshu#10828), written in the same
// transaction. After the work-item insert, and after the slot move when advance
// is set, the admission upserts one plan row for each plan whose scope the insert
// accepted, then commits. A plan for a chunk the terminal-run skip, the
// open-target guard, or the insert dropped writes no row, so the row never names
// a work item that does not exist, and a rolled-back admission leaves neither.
//
// The plan rows share the advisory lock the admission already takes for the
// work items' (collector_kind, collector_instance_id): no new advisory lock, and none is
// held across planning. A nil advance moves no slots. The store refuses a plan
// list it cannot trust before it opens a transaction (see
// validateBaselinePlans).
//
// The counts are reported in [workflow.RunAdmission.BaselinePlansRecorded] and,
// once per admission after the commit, on eshu_dp_nvd_baseline_plan_rows_total.
func (s *WorkflowControlStore) CreateRunWithWorkItemsRecordingBaselinePlans(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
	plans []workflow.NVDBaselinePlan,
	advance *workflow.OwnedTargetSlotAdvance,
) (workflow.RunAdmission, workflow.SlotAdvanceResult, error) {
	return s.createRunWithWorkItemsIfNoOpenTargets(ctx, run, items, advance, plans)
}

// toBaselinePlanRows converts the coordinator's plans to the store's row type.
// The two types stay separate because internal/workflow imports reducer code
// the vulnerability store must not depend on.
func toBaselinePlanRows(plans []workflow.NVDBaselinePlan) []vulnerabilitystore.NVDBaselinePlan {
	rows := make([]vulnerabilitystore.NVDBaselinePlan, len(plans))
	for i, plan := range plans {
		rows[i] = vulnerabilitystore.NVDBaselinePlan{BaselineRunID: plan.BaselineRunID, Year: plan.Year, ScopeID: plan.ScopeID}
	}
	return rows
}

// validateBaselinePlans refuses a plan list the admission cannot trust, before
// any transaction opens. Each plan must name a scope the admitted items carry,
// that scope must parse as an NVD baseline scope whose run and year match the
// plan, and its item must be a vulnerability_intelligence work item. The store
// re-derives the run and year from the scope id on purpose: the coordinator
// builds the plan, and a plan that disagreed with its own scope id would write a
// pointer to the wrong year silently. A duplicate (run, year), a blank field, or
// a non-positive year is refused with the writer's own check, because the
// upsert would otherwise abort mid-statement.
func validateBaselinePlans(plans []workflow.NVDBaselinePlan, items []workflow.WorkItem) error {
	if len(plans) == 0 {
		return nil
	}
	if err := vulnerabilitystore.ValidateNVDBaselinePlans(toBaselinePlanRows(plans)); err != nil {
		return fmt.Errorf("create guarded workflow run: %w", err)
	}
	admitted := make(map[string]struct{}, len(items))
	vulnerability := make(map[string]struct{}, len(items))
	for _, item := range items {
		admitted[item.ScopeID] = struct{}{}
		if item.CollectorKind == scope.CollectorVulnerabilityIntelligence {
			vulnerability[item.ScopeID] = struct{}{}
		}
	}
	for _, plan := range plans {
		if _, ok := admitted[plan.ScopeID]; !ok {
			return fmt.Errorf("create guarded workflow run: baseline plan scope %q is not in the admission", plan.ScopeID)
		}
		runID, year, _, _, ok := vulnsource.ParseNVDBaselineScope(plan.ScopeID)
		if !ok {
			return fmt.Errorf("create guarded workflow run: baseline plan scope %q is not an NVD baseline scope", plan.ScopeID)
		}
		if runID != plan.BaselineRunID || year != plan.Year {
			return fmt.Errorf("create guarded workflow run: baseline plan (run %q, year %d) disagrees with its scope id (run %q, year %d)",
				plan.BaselineRunID, plan.Year, runID, year)
		}
		if _, ok := vulnerability[plan.ScopeID]; !ok {
			return fmt.Errorf("create guarded workflow run: baseline plan scope %q is not a vulnerability_intelligence work item", plan.ScopeID)
		}
	}
	return nil
}

// recordBaselinePlansForInserted upserts the plan rows for the plans whose scope
// the work-item insert accepted, on the admission transaction. It returns the
// rows written. A plan whose chunk the insert dropped writes nothing. Fewer or
// more rows written than the accepted plans is an error that aborts the
// admission: a missing plan row would let the planner mint a second chunk for
// the year, and the work items must not commit without it.
func recordBaselinePlansForInserted(
	ctx context.Context,
	executor db.Executor,
	plans []workflow.NVDBaselinePlan,
	insertedScopeIDs []string,
) (int, error) {
	if len(plans) == 0 || len(insertedScopeIDs) == 0 {
		return 0, nil
	}
	inserted := make(map[string]struct{}, len(insertedScopeIDs))
	for _, scopeID := range insertedScopeIDs {
		inserted[scopeID] = struct{}{}
	}
	landed := make([]workflow.NVDBaselinePlan, 0, len(plans))
	for _, plan := range plans {
		if _, ok := inserted[plan.ScopeID]; ok {
			landed = append(landed, plan)
		}
	}
	written, err := vulnerabilitystore.RecordNVDBaselinePlans(ctx, executor, toBaselinePlanRows(landed))
	if err != nil {
		return 0, fmt.Errorf("create guarded workflow run: %w", err)
	}
	if written != len(landed) {
		return 0, fmt.Errorf("create guarded workflow run: wrote %d of %d baseline plan rows", written, len(landed))
	}
	return written, nil
}

// countBaselinePlanRows records the admission's plan-row outcome after the
// commit: the rows written as recorded and the plans handed in but not written
// as not_inserted. It runs once per admission, never inside the advisory-locked
// transaction, and never for a rolled-back admission. An admission handed no
// plans records nothing.
func (s *WorkflowControlStore) countBaselinePlanRows(ctx context.Context, supplied, recorded int) {
	if s.Instruments == nil || supplied == 0 {
		return
	}
	s.Instruments.RecordNVDBaselinePlanRows(ctx, int64(recorded), int64(supplied-recorded))
}
