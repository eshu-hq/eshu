// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"strings"
	"time"

	packages "github.com/eshu-hq/eshu/go/internal/coordinator/registry/package"
	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s Service) schedulePackageRegistryWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldSchedulePackageRegistry(instance) {
			continue
		}
		if s.PackageRegistryPlanner == nil {
			return fmt.Errorf("package registry planner is required for active package_registry collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		ownedTargets, err := s.packageRegistryOwnedTargets(ctx, instance, observedAt, interval)
		if err != nil {
			return fmt.Errorf("load package registry derived targets for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.PackageRegistryPlanner.PlanPackageRegistryWork(ctx, packages.PlanRequest{
			Instance:            instance,
			ObservedAt:          observedAt,
			PlanKey:             s.packageRegistryPlanKey(instance, observedAt, interval),
			OwnedPackageTargets: ownedTargets,
		})
		if err != nil {
			return fmt.Errorf("plan package registry work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create package registry scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func (s Service) packageRegistryOwnedTargets(
	ctx context.Context,
	instance workflow.CollectorInstance,
	observedAt time.Time,
	interval time.Duration,
) ([]workflow.OwnedPackageDependencyTarget, error) {
	derivation, err := packages.DerivationFromConfig(instance.Configuration)
	if err != nil {
		return nil, err
	}
	if !derivation.Enabled {
		return nil, nil
	}
	if s.OwnedPackageTargetReader == nil {
		return nil, fmt.Errorf("owned package target reader is required for derived package registry targets")
	}
	targetLimit := packages.DerivedTargetLimit(derivation.TargetLimit)
	return s.OwnedPackageTargetReader.ListOwnedPackageDependencyTargets(ctx, workflow.OwnedPackageDependencyTargetFilter{
		Ecosystems:     schedule.SortedStringSetValues(packages.DerivationEcosystems(derivation.Ecosystems)),
		Limit:          schedule.DerivedTargetReadLimit(targetLimit),
		RotationOffset: schedule.DerivedTargetRotationOffsetForMode(derivation.PlanningMode, observedAt, interval, targetLimit),
	})
}

func shouldSchedulePackageRegistry(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorPackageRegistry &&
		instance.Enabled &&
		instance.ClaimsEnabled
}

func (s Service) packageRegistryPlanKey(instance workflow.CollectorInstance, observedAt time.Time, interval time.Duration) string {
	if instance.Bootstrap {
		return "bootstrap"
	}
	prefix := strings.TrimSpace(string(instance.Mode))
	derivation, _ := packages.DerivationFromConfig(instance.Configuration)
	return schedule.DerivedTargetPlanKey(prefix, observedAt, interval, derivation.PlanningMode)
}
