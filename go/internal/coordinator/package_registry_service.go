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
		ownedTargets, err := s.packageRegistryOwnedTargets(ctx, instance, observedAt)
		if err != nil {
			return fmt.Errorf("load package registry derived targets for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.PackageRegistryPlanner.PlanPackageRegistryWork(ctx, PackageRegistryPlanRequest{
			Instance:            instance,
			ObservedAt:          observedAt,
			PlanKey:             s.packageRegistryPlanKey(instance, observedAt),
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
) ([]workflow.OwnedPackageDependencyTarget, error) {
	derivation, err := packageRegistryDerivationFromConfig(instance.Configuration)
	if err != nil {
		return nil, err
	}
	if !derivation.Enabled {
		return nil, nil
	}
	if s.OwnedPackageTargetReader == nil {
		return nil, fmt.Errorf("owned package target reader is required for derived package registry targets")
	}
	targetLimit := packageRegistryDerivedTargetLimit(derivation.TargetLimit)
	return s.OwnedPackageTargetReader.ListOwnedPackageDependencyTargets(ctx, workflow.OwnedPackageDependencyTargetFilter{
		Ecosystems:     sortedStringSetValues(packageRegistryDerivationEcosystems(derivation.Ecosystems)),
		Limit:          derivedTargetReadLimit(targetLimit),
		RotationOffset: derivedTargetRotationOffsetForMode(derivation.PlanningMode, observedAt, s.Config.ReconcileInterval, targetLimit),
	})
}

func shouldSchedulePackageRegistry(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorPackageRegistry &&
		instance.Enabled &&
		instance.ClaimsEnabled
}

func (s Service) packageRegistryPlanKey(instance workflow.CollectorInstance, observedAt time.Time) string {
	if instance.Bootstrap {
		return "bootstrap"
	}
	interval := s.Config.ReconcileInterval
	prefix := strings.TrimSpace(string(instance.Mode))
	derivation, _ := packageRegistryDerivationFromConfig(instance.Configuration)
	return derivedTargetPlanKey(prefix, observedAt, interval, derivation.PlanningMode)
}
