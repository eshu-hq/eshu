// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/component/activation"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/pagerduty"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// PagerDutyPlanner plans PagerDuty incident-evidence workflow rows from
// collector instance configuration.
type PagerDutyPlanner interface {
	PlanPagerDutyWork(context.Context, pagerduty.PlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

func (s Service) schedulePagerDutyWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldSchedulePagerDuty(instance) {
			continue
		}
		if s.PagerDutyPlanner == nil {
			return fmt.Errorf("pagerduty planner is required for active pagerduty collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.PagerDutyPlanner.PlanPagerDutyWork(ctx, pagerduty.PlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    scheduledPlanKey(instance, observedAt, interval),
		})
		if err != nil {
			return fmt.Errorf("plan pagerduty work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create pagerduty scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldSchedulePagerDuty(instance workflow.CollectorInstance) bool {
	if instance.CollectorKind != scope.CollectorPagerDuty || !instance.Enabled || !instance.ClaimsEnabled {
		return false
	}
	if _, ok, err := activation.ParseConfig(instance.Configuration); ok || err != nil {
		return false
	}
	return true
}
