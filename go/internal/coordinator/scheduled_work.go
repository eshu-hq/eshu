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
	enqueued, err := s.Store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
	if err != nil {
		return 0, err
	}
	if enqueued < len(items) && s.Logger != nil {
		s.Logger.Info(
			"workflow coordinator skipped duplicate workflow work",
			"collector_kind", instance.CollectorKind,
			"collector_instance_id", instance.InstanceID,
			"trigger_kind", run.TriggerKind,
			"planned_work_items", len(items),
			"enqueued_work_items", enqueued,
			"skipped_work_items", len(items)-enqueued,
			"reason", "target_already_planned",
		)
	}
	return enqueued, nil
}
