// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s Service) scheduleTerraformStateWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleTerraformState(instance) {
			continue
		}
		if s.TerraformStatePlanner == nil {
			return fmt.Errorf("terraform state planner is required for active terraform_state collectors")
		}
		run, items, err := s.TerraformStatePlanner.PlanTerraformStateWork(ctx, TerraformStatePlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    s.terraformStatePlanKey(instance, observedAt),
		})
		if err != nil {
			if terraformstate.IsWaitingOnGitGeneration(err) {
				s.logTerraformStateWait(instance, err)
				continue
			}
			return fmt.Errorf("plan terraform state work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create terraform state scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleTerraformState(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorTerraformState &&
		instance.Enabled &&
		instance.ClaimsEnabled
}

func (s Service) terraformStatePlanKey(instance workflow.CollectorInstance, observedAt time.Time) string {
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

func (s Service) logTerraformStateWait(instance workflow.CollectorInstance, err error) {
	if s.Logger == nil {
		return
	}
	s.Logger.Info(
		"terraform state workflow planning waiting on git generation",
		"collector_instance_id", instance.InstanceID,
		"collector_kind", instance.CollectorKind,
		"status", "waiting_on_git_generation",
		"error", err.Error(),
	)
}
