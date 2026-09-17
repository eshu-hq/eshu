// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/loki"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// LokiPlanner plans Loki observability workflow rows from collector instance
// configuration.
type LokiPlanner interface {
	PlanLokiWork(context.Context, loki.PlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// scheduleLokiWork plans and admits one work item per enabled Loki target for
// every active, claim-enabled Loki collector instance. It is a no-op outside
// active mode or when claims are disabled. Admission is idempotent: the store
// rejects duplicate targets that an open run already owns.
func (s Service) scheduleLokiWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleLoki(instance) {
			continue
		}
		if s.LokiPlanner == nil {
			return fmt.Errorf("loki planner is required for active loki collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.LokiPlanner.PlanLokiWork(ctx, loki.PlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    scheduledPlanKey(instance, observedAt, interval),
		})
		if err != nil {
			return fmt.Errorf("plan loki work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create loki scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleLoki(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorLoki &&
		instance.Enabled &&
		instance.ClaimsEnabled
}
