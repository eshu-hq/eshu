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

	"github.com/eshu-hq/eshu/go/internal/collector/vaultlive"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// VaultLivePlanRequest carries one Vault metadata planning request.
type VaultLivePlanRequest struct {
	Instance   workflow.CollectorInstance
	ObservedAt time.Time
	PlanKey    string
}

// VaultLiveWorkPlanner plans workflow rows for configured Vault metadata
// targets without resolving credentials or contacting Vault.
type VaultLiveWorkPlanner struct{}

type vaultLiveRuntimeConfiguration struct {
	Targets []vaultLiveTargetConfiguration `json:"targets"`
}

type vaultLiveTargetConfiguration struct {
	VaultClusterID string `json:"vault_cluster_id"`
	Namespace      string `json:"namespace"`
	DisplayName    string `json:"display_name"`
	Environment    string `json:"environment"`
}

// PlanVaultLiveWork returns one run and one work item per configured Vault
// metadata target.
func (p VaultLiveWorkPlanner) PlanVaultLiveWork(
	_ context.Context,
	request VaultLivePlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validateVaultLivePlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	targets, err := parseVaultLiveRuntimeTargets(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	if err := validateUniqueVaultLiveTargets(targets); err != nil {
		return workflow.Run{}, nil, err
	}

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              vaultLiveRunID(request.Instance, request.PlanKey),
		TriggerKind:        vaultLiveTriggerKind(request.Instance),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  vaultLiveRequestedScopeSet(request.Instance, targets),
		RequestedCollector: string(scope.CollectorVaultLive),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for _, target := range targets {
		item, err := vaultLiveWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validateVaultLivePlanRequest(request VaultLivePlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("vault live plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorVaultLive {
		return fmt.Errorf("vault live planner requires collector_kind %q", scope.CollectorVaultLive)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("vault live planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("vault live planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("vault live planner observed_at must not be zero")
	}
	return validateSafePlanKey("vault live planner", request.PlanKey)
}

func parseVaultLiveRuntimeTargets(raw string) ([]vaultLiveTargetConfiguration, error) {
	if err := workflow.ValidateVaultLiveCollectorConfiguration(raw); err != nil {
		return nil, err
	}
	var decoded vaultLiveRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("decode vault live collector configuration: %w", err)
	}
	targets := make([]vaultLiveTargetConfiguration, 0, len(decoded.Targets))
	for _, target := range decoded.Targets {
		target.VaultClusterID = strings.TrimSpace(target.VaultClusterID)
		target.Namespace = strings.TrimSpace(target.Namespace)
		target.DisplayName = strings.TrimSpace(target.DisplayName)
		target.Environment = strings.TrimSpace(target.Environment)
		targets = append(targets, target)
	}
	return targets, nil
}

func validateUniqueVaultLiveTargets(targets []vaultLiveTargetConfiguration) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		key := vaultLiveTargetKey(target)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate vault live target")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func vaultLiveRunID(instance workflow.CollectorInstance, planKey string) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorVaultLive,
		strings.TrimSpace(instance.InstanceID),
		vaultLiveTriggerKind(instance),
		strings.TrimSpace(planKey),
	)
}

func vaultLiveTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

func vaultLiveRequestedScopeSet(
	instance workflow.CollectorInstance,
	targets []vaultLiveTargetConfiguration,
) string {
	type requestedTarget struct {
		ScopeID     string `json:"scope_id"`
		Environment string `json:"environment,omitempty"`
	}
	payload := struct {
		CollectorInstanceID string            `json:"collector_instance_id"`
		Targets             []requestedTarget `json:"targets"`
	}{
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		Targets:             make([]requestedTarget, 0, len(targets)),
	}
	for _, target := range targets {
		scopeID, err := vaultlive.VaultScopeID(target.VaultClusterID, target.Namespace)
		if err != nil {
			continue
		}
		payload.Targets = append(payload.Targets, requestedTarget{
			ScopeID:     scopeID,
			Environment: strings.TrimSpace(target.Environment),
		})
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

func vaultLiveWorkItem(
	instance workflow.CollectorInstance,
	target vaultLiveTargetConfiguration,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	scopeID, err := vaultlive.VaultScopeID(target.VaultClusterID, target.Namespace)
	if err != nil {
		return workflow.WorkItem{}, err
	}
	generationID := "vault_live:" + facts.StableID("VaultLiveWorkflowGeneration", map[string]any{
		"instance_id": strings.TrimSpace(instance.InstanceID),
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    scopeID,
	})
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorVaultLive, strings.TrimSpace(instance.InstanceID), generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorVaultLive,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        vaultlive.CollectorKind,
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorVaultLive, strings.TrimSpace(instance.InstanceID), scopeID),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}

func vaultLiveTargetKey(target vaultLiveTargetConfiguration) string {
	return strings.TrimSpace(target.VaultClusterID) + "\x00" + strings.TrimSpace(target.Namespace)
}
