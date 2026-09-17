// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator //nolint:dirgate // Vault scheduling and durable admission remain root Service ownership.

import (
	"context"
	"fmt"
	"time"

	coordinatorvaultlive "github.com/eshu-hq/eshu/go/internal/coordinator/vault/live"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// VaultLivePlanner plans live Vault metadata workflow rows from collector
// instance configuration.
type VaultLivePlanner interface {
	PlanVaultLiveWork(context.Context, coordinatorvaultlive.PlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

func (s Service) scheduleVaultLiveWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleVaultLive(instance) {
			continue
		}
		if s.VaultLivePlanner == nil {
			return fmt.Errorf("vault live planner is required for active vault live collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.VaultLivePlanner.PlanVaultLiveWork(ctx, coordinatorvaultlive.PlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    scheduledPlanKey(instance, observedAt, interval),
		})
		if err != nil {
			return fmt.Errorf("plan vault live work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create vault live scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleVaultLive(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorVaultLive &&
		instance.Enabled &&
		instance.ClaimsEnabled
}
