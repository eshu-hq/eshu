// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ComponentExtensionPlanRequest carries one generic component extension
// planning request. The coordinator never resolves component credentials or
// executes component artifacts while planning.
type ComponentExtensionPlanRequest struct {
	Instance   workflow.CollectorInstance
	ObservedAt time.Time
	PlanKey    string
}

// ComponentExtensionWorkPlanner plans one activation-scoped workflow item for
// a claim-capable component extension instance.
type ComponentExtensionWorkPlanner struct{}

// PlanComponentExtensionWork returns one collection-pending run and one work
// item for the activated component instance. The tuple is deterministic for
// the component, instance, manifest digest, config handle, and plan key so
// repeated coordinator reconciles remain idempotent.
func (p ComponentExtensionWorkPlanner) PlanComponentExtensionWork(
	_ context.Context,
	request ComponentExtensionPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	config, err := validateComponentExtensionPlanRequest(request)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              componentExtensionRunID(request.Instance, config, request.PlanKey),
		TriggerKind:        componentExtensionTriggerKind(request.Instance),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  componentExtensionRequestedScopeSet(request.Instance, config),
		RequestedCollector: string(request.Instance.CollectorKind),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	item := componentExtensionWorkItem(request.Instance, config, run.RunID, request.PlanKey, observedAt)
	return run, []workflow.WorkItem{item}, nil
}

func validateComponentExtensionPlanRequest(
	request ComponentExtensionPlanRequest,
) (componentInstanceConfig, error) {
	if err := request.Instance.Validate(); err != nil {
		return componentInstanceConfig{}, fmt.Errorf("component extension plan request: %w", err)
	}
	if !request.Instance.Enabled {
		return componentInstanceConfig{}, fmt.Errorf("component extension planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return componentInstanceConfig{}, fmt.Errorf("component extension planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return componentInstanceConfig{}, fmt.Errorf("component extension planner observed_at must not be zero")
	}
	if err := validateSafePlanKey("component extension planner", request.PlanKey); err != nil {
		return componentInstanceConfig{}, err
	}
	config, ok, err := parseComponentInstanceConfig(request.Instance.Configuration)
	if err != nil {
		return componentInstanceConfig{}, err
	}
	if !ok {
		return componentInstanceConfig{}, fmt.Errorf("component extension planner requires component activation configuration")
	}
	return config, nil
}

func componentExtensionRunID(
	instance workflow.CollectorInstance,
	config componentInstanceConfig,
	planKey string,
) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		strings.TrimSpace(string(instance.CollectorKind)),
		strings.TrimSpace(instance.InstanceID),
		componentExtensionTriggerKind(instance),
		componentExtensionIdentity(config, planKey),
	)
}

func componentExtensionTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

func componentExtensionRequestedScopeSet(
	instance workflow.CollectorInstance,
	config componentInstanceConfig,
) string {
	payload := struct {
		CollectorInstanceID string                                 `json:"collector_instance_id"`
		CollectorKind       string                                 `json:"collector_kind"`
		ComponentID         string                                 `json:"component_id"`
		ComponentVersion    string                                 `json:"component_version"`
		ManifestDigest      string                                 `json:"manifest_digest"`
		ConfigHandle        string                                 `json:"config_handle"`
		Host                *component.ActivationHostClaimMetadata `json:"host,omitempty"`
		Runtime             struct {
			SDKProtocol string `json:"sdk_protocol"`
			Adapter     string `json:"adapter"`
		} `json:"runtime"`
	}{
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		CollectorKind:       strings.TrimSpace(string(instance.CollectorKind)),
		ComponentID:         strings.TrimSpace(config.ComponentID),
		ComponentVersion:    strings.TrimSpace(config.ComponentVersion),
		ManifestDigest:      strings.TrimSpace(config.ManifestDigest),
		ConfigHandle:        strings.TrimSpace(config.ConfigHandle),
	}
	if host, ok := componentExtensionHostClaim(config); ok {
		payload.Host = &host
	}
	payload.Runtime.SDKProtocol = strings.TrimSpace(config.Runtime.SDKProtocol)
	payload.Runtime.Adapter = strings.TrimSpace(config.Runtime.Adapter)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func componentExtensionWorkItem(
	instance workflow.CollectorInstance,
	config componentInstanceConfig,
	runID string,
	planKey string,
	observedAt time.Time,
) workflow.WorkItem {
	sourceSystem, scopeID := componentExtensionClaimIdentity(config)
	// A component generation IS its run: the claimed-collection runtime invariant
	// for non-terraform kinds requires GenerationID == SourceRunID (see
	// collector.validateClaimedGeneration). Mint a single identity for both, as
	// every other non-terraform scheduler does.
	generationID := "component-generation:" + componentExtensionIdentity(config, planKey)
	sourceRunID := generationID
	return workflow.WorkItem{
		WorkItemID:          "component-work:" + componentExtensionIdentity(config, planKey),
		RunID:               runID,
		CollectorKind:       instance.CollectorKind,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        sourceSystem,
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         sourceRunID,
		GenerationID:        generationID,
		FairnessKey: fmt.Sprintf(
			"%s:%s:%s:%s",
			strings.TrimSpace(string(instance.CollectorKind)),
			strings.TrimSpace(instance.InstanceID),
			sourceSystem,
			scopeID,
		),
		Status:    workflow.WorkItemStatusPending,
		VisibleAt: observedAt,
		CreatedAt: observedAt,
		UpdatedAt: observedAt,
	}
}

func componentExtensionClaimIdentity(config componentInstanceConfig) (string, string) {
	if host, ok := componentExtensionHostClaim(config); ok {
		return host.SourceSystem, host.Scope.ID
	}
	scopeID := "component:" + componentExtensionIdentity(config, "")
	return strings.TrimSpace(config.ComponentID), scopeID
}

func componentExtensionHostClaim(config componentInstanceConfig) (component.ActivationHostClaimMetadata, bool) {
	if config.Host == nil {
		return component.ActivationHostClaimMetadata{}, false
	}
	host := config.Host.Normalized()
	if host.Empty() {
		return component.ActivationHostClaimMetadata{}, false
	}
	return host, true
}

func componentExtensionIdentity(config componentInstanceConfig, planKey string) string {
	identity := map[string]any{
		"component_id":      strings.TrimSpace(config.ComponentID),
		"component_version": strings.TrimSpace(config.ComponentVersion),
		"manifest_digest":   strings.TrimSpace(config.ManifestDigest),
		"config_handle":     strings.TrimSpace(config.ConfigHandle),
		"plan_key":          strings.TrimSpace(planKey),
	}
	if host, ok := componentExtensionHostClaim(config); ok {
		identity["host_source_system"] = host.SourceSystem
		identity["host_scope_id"] = host.Scope.ID
		identity["host_scope_kind"] = host.Scope.Kind
	}
	return facts.StableID("ComponentExtensionWorkflow", identity)
}
