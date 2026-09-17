// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/metrics"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// PrometheusMimirPlanner plans Prometheus/Mimir metric-metadata workflow rows
// from collector instance configuration.
type PrometheusMimirPlanner interface {
	PlanPrometheusMimirWork(context.Context, metrics.PlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

// schedulePrometheusMimirWork admits one scheduled run per active, claim-enabled
// Prometheus/Mimir collector instance. It is a no-op outside active mode or when
// claims are disabled, and it skips instances with no enabled target so empty
// configuration never creates an empty run.
func (s Service) schedulePrometheusMimirWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldSchedulePrometheusMimir(instance) {
			continue
		}
		if s.PrometheusMimirPlanner == nil {
			return fmt.Errorf("prometheus/mimir planner is required for active prometheus_mimir collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.PrometheusMimirPlanner.PlanPrometheusMimirWork(ctx, metrics.PlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    scheduledPlanKey(instance, observedAt, interval),
		})
		if err != nil {
			return fmt.Errorf("plan prometheus/mimir work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create prometheus/mimir scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldSchedulePrometheusMimir(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorPrometheusMimir &&
		instance.Enabled &&
		instance.ClaimsEnabled
}
