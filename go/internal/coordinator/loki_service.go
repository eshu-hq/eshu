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
		run, items, err := s.LokiPlanner.PlanLokiWork(ctx, LokiPlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    s.lokiPlanKey(instance, observedAt),
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

func (s Service) lokiPlanKey(instance workflow.CollectorInstance, observedAt time.Time) string {
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
