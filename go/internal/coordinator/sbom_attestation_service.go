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
		run, items, err := s.SBOMAttestationPlanner.PlanSBOMAttestationWork(ctx, SBOMAttestationPlanRequest{
			Instance:   instance,
			ObservedAt: observedAt,
			PlanKey:    s.sbomAttestationPlanKey(instance, observedAt),
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

func (s Service) sbomAttestationPlanKey(instance workflow.CollectorInstance, observedAt time.Time) string {
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
