// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

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
		run, items, err := s.PagerDutyPlanner.PlanPagerDutyWork(ctx, PagerDutyPlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    s.pagerDutyPlanKey(instance, observedAt),
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
	if _, ok, err := parseComponentInstanceConfig(instance.Configuration); ok || err != nil {
		return false
	}
	return true
}

func (s Service) pagerDutyPlanKey(instance workflow.CollectorInstance, observedAt time.Time) string {
	if instance.Bootstrap {
		return "bootstrap"
	}
	interval := s.Config.ReconcileInterval
	if interval <= 0 {
		interval = defaultReconcileInterval
	}
	prefix := strings.TrimSpace(string(instance.Mode))
	if prefix == "" {
		prefix = "schedule"
	}
	return fmt.Sprintf("%s-%s", prefix, observedAt.UTC().Truncate(interval).Format("20060102T150405Z"))
}
