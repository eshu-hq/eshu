// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/tempo"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TempoPlanner plans Tempo trace-signal workflow rows from collector instance
// configuration.
type TempoPlanner interface {
	PlanTempoWork(context.Context, tempo.PlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

func (s Service) scheduleTempoWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleTempo(instance) {
			continue
		}
		if s.TempoPlanner == nil {
			return fmt.Errorf("tempo planner is required for active tempo collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.TempoPlanner.PlanTempoWork(ctx, tempo.PlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    scheduledPlanKey(instance, observedAt, interval),
		})
		if err != nil {
			return fmt.Errorf("plan tempo work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create tempo scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleTempo(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorTempo &&
		instance.Enabled &&
		instance.ClaimsEnabled
}
