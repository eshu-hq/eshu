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

func (s Service) scheduleAWSScheduledWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleAWS(instance) {
			continue
		}
		enabled, err := awsScheduledScanEnabled(instance.Configuration)
		if err != nil {
			return fmt.Errorf("read AWS scheduled scan config for %q: %w", instance.InstanceID, err)
		}
		if !enabled {
			continue
		}
		if s.AWSScheduledPlanner == nil {
			return fmt.Errorf("AWS scheduled planner is required for active aws collectors")
		}
		run, items, err := s.AWSScheduledPlanner.PlanAWSScheduledWork(ctx, AWSScheduledPlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    s.awsScheduledPlanKey(instance, observedAt),
		})
		if err != nil {
			return fmt.Errorf("plan AWS scheduled work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 && run.Status != workflow.RunStatusComplete {
			continue
		}
		if len(items) == 0 {
			if err := s.Store.CreateRun(ctx, run); err != nil {
				return fmt.Errorf("create AWS scheduled workflow run for %q: %w", instance.InstanceID, err)
			}
			if s.Logger != nil {
				s.Logger.Info(
					"aws scheduled workflow run recorded skipped targets only",
					"collector_instance_id", instance.InstanceID,
					"collector_kind", instance.CollectorKind,
					"run_id", run.RunID,
					"status", run.Status,
				)
			}
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create AWS scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleAWS(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorAWS &&
		instance.Enabled &&
		instance.ClaimsEnabled
}

func (s Service) awsScheduledPlanKey(instance workflow.CollectorInstance, observedAt time.Time) string {
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
