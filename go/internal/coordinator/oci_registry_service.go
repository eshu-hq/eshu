// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator //nolint:dirgate // OCI registry scheduling and durable admission remain on root Service methods.

import (
	"context"
	"fmt"
	"time"

	ociregistry "github.com/eshu-hq/eshu/go/internal/coordinator/oci/registry"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s Service) scheduleOCIRegistryWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleOCIRegistry(instance) {
			continue
		}
		if s.OCIRegistryPlanner == nil {
			return fmt.Errorf("OCI registry planner is required for active oci_registry collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.OCIRegistryPlanner.PlanOCIRegistryWork(ctx, ociregistry.PlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    scheduledPlanKey(instance, observedAt, interval),
		})
		if err != nil {
			return fmt.Errorf("plan OCI registry work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create OCI registry scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleOCIRegistry(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorOCIRegistry &&
		instance.Enabled &&
		instance.ClaimsEnabled
}
