// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/tfstate"
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
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.TerraformStatePlanner.PlanTerraformStateWork(ctx, tfstate.PlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    scheduledPlanKey(instance, observedAt, interval),
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
