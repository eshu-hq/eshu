// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator //nolint:dirgate // SBOM scheduling and durable admission remain on root Service methods.

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/sbom/attestation"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// SBOMAttestationPlanner plans hosted SBOM/attestation workflow rows from
// collector instance configuration.
type SBOMAttestationPlanner interface {
	PlanSBOMAttestationWork(context.Context, attestation.PlanRequest) (workflow.Run, []workflow.WorkItem, error)
}

func (s Service) scheduleSBOMAttestationWork(
	ctx context.Context,
	observedAt time.Time,
	instances []workflow.CollectorInstance,
) error {
	if s.Config.DeploymentMode != deploymentModeActive || !s.Config.ClaimsEnabled {
		return nil
	}
	for _, instance := range instances {
		if !shouldScheduleSBOMAttestation(instance) {
			continue
		}
		if s.SBOMAttestationPlanner == nil {
			return fmt.Errorf("SBOM attestation planner is required for active sbom_attestation collectors")
		}
		interval, err := s.scanInterval(instance)
		if err != nil {
			return fmt.Errorf("read scan interval for %q: %w", instance.InstanceID, err)
		}
		run, items, err := s.SBOMAttestationPlanner.PlanSBOMAttestationWork(ctx, attestation.PlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    scheduledPlanKey(instance, observedAt, interval),
		})
		if err != nil {
			return fmt.Errorf("plan SBOM attestation work for %q: %w", instance.InstanceID, err)
		}
		if len(items) == 0 {
			continue
		}
		if _, err := s.createWorkflowWorkIfNoOpenTargets(ctx, instance, run, items); err != nil {
			return fmt.Errorf("create SBOM attestation scheduled work for %q: %w", instance.InstanceID, err)
		}
	}
	return nil
}

func shouldScheduleSBOMAttestation(instance workflow.CollectorInstance) bool {
	return instance.CollectorKind == scope.CollectorSBOMAttestation &&
		instance.Enabled &&
		instance.ClaimsEnabled
}
