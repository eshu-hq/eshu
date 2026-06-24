// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func (s Service) vulnerabilityInstalledEvidenceTargets(
	ctx context.Context,
	instance workflow.CollectorInstance,
	observedAt time.Time,
) ([]workflow.OSPackageAdvisoryTarget, []workflow.SBOMComponentAdvisoryTarget, error) {
	derivation, err := vulnerabilityInstalledEvidenceDerivationFromConfig(instance.Configuration)
	if err != nil {
		return nil, nil, err
	}
	if !derivation.Enabled {
		return nil, nil, nil
	}
	if s.OSPackageAdvisoryTargetReader == nil && s.SBOMComponentAdvisoryTargetReader == nil {
		return nil, nil, fmt.Errorf("installed evidence target reader is required for derived vulnerability targets")
	}
	targetLimit := vulnerabilityDerivedTargetLimit(derivation.TargetLimit)
	filter := workflow.OSPackageAdvisoryTargetFilter{
		Ecosystems:     sortedStringSetValues(derivationEcosystems(derivation.Ecosystems, []string{"npm"})),
		Limit:          derivedTargetReadLimit(targetLimit),
		RotationOffset: derivedTargetRotationOffsetForMode(derivation.PlanningMode, observedAt, s.Config.ReconcileInterval, targetLimit),
	}
	var osTargets []workflow.OSPackageAdvisoryTarget
	if s.OSPackageAdvisoryTargetReader != nil {
		var err error
		osTargets, err = s.OSPackageAdvisoryTargetReader.ListOSPackageAdvisoryTargets(ctx, filter)
		if err != nil {
			return nil, nil, fmt.Errorf("list OS package advisory targets: %w", err)
		}
	}
	if s.SBOMComponentAdvisoryTargetReader == nil {
		return osTargets, nil, nil
	}
	sbomFilter := workflow.SBOMComponentAdvisoryTargetFilter(filter)
	sbomTargets, err := s.SBOMComponentAdvisoryTargetReader.ListSBOMComponentAdvisoryTargets(ctx, sbomFilter)
	if err != nil {
		return nil, nil, fmt.Errorf("list SBOM component advisory targets: %w", err)
	}
	return osTargets, sbomTargets, nil
}

func vulnerabilityInstalledEvidenceDerivationFromConfig(raw string) (vulnerabilityDerivationConfiguration, error) {
	if err := workflow.ValidateVulnerabilityIntelligenceCollectorConfiguration(raw); err != nil {
		return vulnerabilityDerivationConfiguration{}, err
	}
	var decoded vulnerabilityRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return vulnerabilityDerivationConfiguration{}, fmt.Errorf("decode vulnerability installed evidence derivation config: %w", err)
	}
	return decoded.DeriveFromInstalledEvidence, nil
}
