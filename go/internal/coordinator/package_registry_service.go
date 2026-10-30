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
		ownedPage, readOwned, err := s.packageRegistryOwnedTargets(ctx, instance, observedAt, interval)
		if err != nil {
			return fmt.Errorf("load package registry derived targets for %q: %w", instance.InstanceID, err)
		}
		s.logOwnedPackageWindow(instance, readOwned, ownedPage)
		run, items, err := s.PackageRegistryPlanner.PlanPackageRegistryWork(ctx, packages.PlanRequest{
			Instance:            instance,
			ObservedAt:          observedAt,
			PlanKey:             s.packageRegistryPlanKey(instance, observedAt, interval),
			OwnedPackageTargets: ownedPage.Targets,
			OwnedPackageWindow:  ownedPage.Window,
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

// packageRegistryOwnedTargets reads one hash slot of the owned-package
// targets for a derived package-registry instance (#9422). The bool reports
// whether a read happened (derivation enabled).
func (s Service) packageRegistryOwnedTargets(
	ctx context.Context,
	instance workflow.CollectorInstance,
	observedAt time.Time,
	interval time.Duration,
) (workflow.OwnedPackageDependencyTargetPage, bool, error) {
	derivation, err := packages.DerivationFromConfig(instance.Configuration)
	if err != nil {
		return workflow.OwnedPackageDependencyTargetPage{}, false, err
	}
	if !derivation.Enabled {
		return workflow.OwnedPackageDependencyTargetPage{}, false, nil
	}
	if s.OwnedPackageTargetReader == nil {
		return workflow.OwnedPackageDependencyTargetPage{}, false, fmt.Errorf("owned package target reader is required for derived package registry targets")
	}
	targetLimit := packages.DerivedTargetLimit(derivation.TargetLimit)
	page, err := s.OwnedPackageTargetReader.ListOwnedPackageDependencyTargets(ctx, workflow.OwnedPackageDependencyTargetFilter{
		Ecosystems:     schedule.SortedStringSetValues(packages.DerivationEcosystems(derivation.Ecosystems)),
		Limit:          schedule.DerivedTargetReadLimit(targetLimit),
		TargetLimit:    schedule.DerivedTargetSlotLimitForMode(derivation.PlanningMode, targetLimit),
		RotationBucket: schedule.DerivedTargetRotationBucketForMode(derivation.PlanningMode, observedAt, interval),
		// Gradle declares Maven coordinates, so the read selects each
		// configured ecosystem's package_manager aliases (#9314), mirroring
		// the vulnerability-intelligence caller.
		ExpandPackageManagerAliases: true,
	})
	return page, true, err
}

func shouldSchedulePackageRegistry(instance workflow.CollectorInstance) bool {
	return inTreeSchedulable(instance, scope.CollectorPackageRegistry)
}

func (s Service) packageRegistryPlanKey(instance workflow.CollectorInstance, observedAt time.Time, interval time.Duration) string {
	if instance.Bootstrap {
		return "bootstrap"
	}
	prefix := strings.TrimSpace(string(instance.Mode))
	derivation, _ := packages.DerivationFromConfig(instance.Configuration)
	return schedule.DerivedTargetPlanKey(prefix, observedAt, interval, derivation.PlanningMode)
}
