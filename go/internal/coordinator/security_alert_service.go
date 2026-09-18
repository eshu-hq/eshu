// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator //nolint:dirgate // Security-alert scheduling and durable admission remain on root Service methods.

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/security/alert"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// SecurityAlertPlanner plans provider security-alert workflow rows from
// collector instance configuration.
type SecurityAlertPlanner interface {
	PlanSecurityAlertWork(context.Context, alert.PlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

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
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.SecurityAlertPlanner.PlanSecurityAlertWork(ctx, alert.PlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    scheduledPlanKey(instance, observedAt, interval),
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
