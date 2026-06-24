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
	enqueued, err := s.Store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, authorizedItems)
	if err != nil {
		return 0, err
	}
	if enqueued < len(authorizedItems) && s.Logger != nil {
		s.Logger.Info(
			"workflow coordinator skipped duplicate workflow work",
			"collector_kind", instance.CollectorKind,
			"collector_instance_id", instance.InstanceID,
			"trigger_kind", run.TriggerKind,
			"planned_work_items", len(authorizedItems),
			"enqueued_work_items", enqueued,
			"skipped_work_items", len(authorizedItems)-enqueued,
			"reason", "target_already_planned",
		)
	}
	return enqueued, nil
}
