// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/grafana"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// GrafanaPlanner plans Grafana observability workflow rows from collector
// instance configuration.
type GrafanaPlanner interface {
	PlanGrafanaWork(context.Context, grafana.PlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

func (s Service) scheduleGrafanaWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleGrafana(instance) {
			continue
		}
		if s.GrafanaPlanner == nil {
			return fmt.Errorf("grafana planner is required for active grafana collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.GrafanaPlanner.PlanGrafanaWork(ctx, grafana.PlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    scheduledPlanKey(instance, observedAt, interval),
		})
		if err != nil {
			return fmt.Errorf("plan grafana work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create grafana scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleGrafana(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorGrafana &&
		instance.Enabled &&
		instance.ClaimsEnabled
}
