// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/jira"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// JiraPlanner plans Jira workflow rows from collector instance configuration.
type JiraPlanner interface {
	PlanJiraWork(context.Context, jira.PlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

func (s Service) scheduleJiraWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleJira(instance) {
			continue
		}
		if s.JiraPlanner == nil {
			return fmt.Errorf("jira planner is required for active jira collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.JiraPlanner.PlanJiraWork(ctx, jira.PlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    scheduledPlanKey(instance, observedAt, interval),
		})
		if err != nil {
			return fmt.Errorf("plan jira work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create jira scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleJira(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorJira &&
		instance.Enabled &&
		instance.ClaimsEnabled
}
