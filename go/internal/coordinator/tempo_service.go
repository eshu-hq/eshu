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
		run, items, err := s.TempoPlanner.PlanTempoWork(ctx, TempoPlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    s.tempoPlanKey(instance, observedAt),
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

func (s Service) tempoPlanKey(instance workflow.CollectorInstance, observedAt time.Time) string {
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
