// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// SBOMAttestationPlanRequest carries one hosted SBOM/attestation planning
// request.
type SBOMAttestationPlanRequest struct {
	Instance   workflow.CollectorInstance
	ObservedAt time.Time
	PlanKey    string
}

// SBOMAttestationWorkPlanner plans workflow rows for configured hosted SBOM
// and attestation targets.
type SBOMAttestationWorkPlanner struct{}

type sbomAttestationRuntimeConfiguration struct {
	Targets []sbomAttestationTargetConfiguration `json:"targets"`
}

type sbomAttestationTargetConfiguration struct {
	ScopeID        string `json:"scope_id"`
	SourceType     string `json:"source_type"`
	ArtifactKind   string `json:"artifact_kind"`
	DocumentFormat string `json:"document_format"`
	Provider       string `json:"provider"`
}

// PlanSBOMAttestationWork returns one run and one work item per configured
// hosted SBOM or attestation target.
func (p SBOMAttestationWorkPlanner) PlanSBOMAttestationWork(
	_ context.Context,
	request SBOMAttestationPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validateSBOMAttestationPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	targets, err := parseSBOMAttestationRuntimeTargets(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	if err := validateUniqueSBOMAttestationTargets(targets); err != nil {
		return workflow.Run{}, nil, err
	}

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              sbomAttestationRunID(request.Instance, request.PlanKey),
		TriggerKind:        sbomAttestationTriggerKind(request.Instance),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  sbomAttestationRequestedScopeSet(request.Instance, targets),
		RequestedCollector: string(scope.CollectorSBOMAttestation),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for _, target := range targets {
		item, err := sbomAttestationWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validateSBOMAttestationPlanRequest(request SBOMAttestationPlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("SBOM attestation plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorSBOMAttestation {
		return fmt.Errorf("SBOM attestation planner requires collector_kind %q", scope.CollectorSBOMAttestation)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("SBOM attestation planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("SBOM attestation planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("SBOM attestation planner observed_at must not be zero")
	}
	if err := validateSafePlanKey("SBOM attestation planner", request.PlanKey); err != nil {
		return err
	}
	return nil
}

func parseSBOMAttestationRuntimeTargets(raw string) ([]sbomAttestationTargetConfiguration, error) {
	if err := workflow.ValidateSBOMAttestationCollectorConfiguration(raw); err != nil {
		return nil, err
	}
	var decoded sbomAttestationRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("decode SBOM attestation collector configuration: %w", err)
	}
	targets := make([]sbomAttestationTargetConfiguration, 0, len(decoded.Targets))
	for _, target := range decoded.Targets {
		target.ScopeID = strings.TrimSpace(target.ScopeID)
		target.SourceType = strings.TrimSpace(target.SourceType)
		target.ArtifactKind = strings.TrimSpace(target.ArtifactKind)
		target.DocumentFormat = strings.TrimSpace(target.DocumentFormat)
		target.Provider = strings.TrimSpace(target.Provider)
		targets = append(targets, target)
	}
	return targets, nil
}

func validateUniqueSBOMAttestationTargets(targets []sbomAttestationTargetConfiguration) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		scopeID := strings.TrimSpace(target.ScopeID)
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate SBOM attestation target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

func sbomAttestationRunID(instance workflow.CollectorInstance, planKey string) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorSBOMAttestation,
		strings.TrimSpace(instance.InstanceID),
		sbomAttestationTriggerKind(instance),
		strings.TrimSpace(planKey),
	)
}

func sbomAttestationTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

func sbomAttestationRequestedScopeSet(
	instance workflow.CollectorInstance,
	targets []sbomAttestationTargetConfiguration,
) string {
	type requestedTarget struct {
		ScopeID        string `json:"scope_id"`
		SourceType     string `json:"source_type"`
		ArtifactKind   string `json:"artifact_kind"`
		DocumentFormat string `json:"document_format"`
		Provider       string `json:"provider,omitempty"`
	}
	payload := struct {
		CollectorInstanceID string            `json:"collector_instance_id"`
		Targets             []requestedTarget `json:"targets"`
	}{
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		Targets:             make([]requestedTarget, 0, len(targets)),
	}
	for _, target := range targets {
		payload.Targets = append(payload.Targets, requestedTarget(target))
	}
	sort.Slice(payload.Targets, func(i, j int) bool {
		return payload.Targets[i].ScopeID < payload.Targets[j].ScopeID
	})
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func sbomAttestationWorkItem(
	instance workflow.CollectorInstance,
	target sbomAttestationTargetConfiguration,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	scopeID := strings.TrimSpace(target.ScopeID)
	generationID := "sbom_attestation:" + facts.StableID("SBOMAttestationWorkflowGeneration", map[string]any{
		"instance_id": strings.TrimSpace(instance.InstanceID),
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    scopeID,
	})
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorSBOMAttestation, instance.InstanceID, generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorSBOMAttestation,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        string(scope.CollectorSBOMAttestation),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorSBOMAttestation, strings.TrimSpace(instance.InstanceID), strings.TrimSpace(target.ArtifactKind)),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}
