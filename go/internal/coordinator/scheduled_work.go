// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s Service) createWorkflowWorkIfNoOpenTargets(
	ctx context.Context,
	instance workflow.CollectorInstance,
	run workflow.Run,
	items []workflow.WorkItem,
) (int, error) {
	authorizedItems, denied, err := s.authorizeWorkflowWorkItems(ctx, run, items)
	if err != nil {
		return 0, err
	}
	if denied > 0 && s.Logger != nil {
		s.Logger.Info(
			"workflow coordinator skipped workflow work by tenant grant",
			"collector_kind", instance.CollectorKind,
			"trigger_kind", run.TriggerKind,
			"planned_work_items", len(items),
			"authorized_work_items", len(authorizedItems),
			"denied_work_items", denied,
			"reason", "tenant_scope_missing_or_stale_policy",
		)
	}
	if len(authorizedItems) == 0 {
		return 0, nil
	}
	if denied > 0 {
		run = filterWorkflowRunRequestedScopeSet(run, authorizedItems)
	}
	admission, err := s.Store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, authorizedItems)
	if err != nil {
		return 0, err
	}
	// The two shortfalls are different events and are logged as such. A target
	// the open-target guard dropped is already being collected by an open run,
	// which is the benign skip that makes a second coordinator safe. A row the
	// guard admitted and the store then refused is work nobody will collect, so
	// it is a warning, not a duplicate notice (#4586).
	if admission.EligibleTargets < len(authorizedItems) && s.Logger != nil {
		s.Logger.Info(
			"workflow coordinator skipped duplicate workflow work",
			"collector_kind", instance.CollectorKind,
			"collector_instance_id", instance.InstanceID,
			"trigger_kind", run.TriggerKind,
			"planned_work_items", len(authorizedItems),
			"enqueued_work_items", admission.InsertedWorkItems,
			"skipped_work_items", len(authorizedItems)-admission.EligibleTargets,
			"reason", "target_already_planned",
		)
	}
	if admission.InsertedWorkItems < admission.EligibleTargets && s.Logger != nil {
		s.Logger.Warn(
			"workflow coordinator lost admitted workflow work at insert",
			"collector_kind", instance.CollectorKind,
			"collector_instance_id", instance.InstanceID,
			"trigger_kind", run.TriggerKind,
			"planned_work_items", len(authorizedItems),
			"admitted_work_items", admission.EligibleTargets,
			"enqueued_work_items", admission.InsertedWorkItems,
			"dropped_work_items", admission.EligibleTargets-admission.InsertedWorkItems,
			"reason", "insert_conflict_dropped_row",
		)
	}
	return admission.InsertedWorkItems, nil
}
