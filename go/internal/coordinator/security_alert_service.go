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

func (s Service) scheduleSecurityAlertWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleSecurityAlert(instance) {
			continue
		}
		if s.SecurityAlertPlanner == nil {
			return fmt.Errorf("security alert planner is required for active security_alert collectors")
		}
		run, items, err := s.SecurityAlertPlanner.PlanSecurityAlertWork(ctx, SecurityAlertPlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    s.securityAlertPlanKey(instance, observedAt),
		})
		if err != nil {
			return fmt.Errorf("plan security alert work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create security alert scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleSecurityAlert(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorSecurityAlert &&
		instance.Enabled &&
		instance.ClaimsEnabled
}

func (s Service) securityAlertPlanKey(instance workflow.CollectorInstance, observedAt time.Time) string {
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
