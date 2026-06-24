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

// CICDRunPlanRequest carries one hosted provider CI/CD run planning request.
type CICDRunPlanRequest struct {
	Instance   workflow.CollectorInstance
	ObservedAt time.Time
	PlanKey    string
}

// CICDRunWorkPlanner plans workflow rows for configured CI/CD run targets
// without resolving credentials or contacting providers.
type CICDRunWorkPlanner struct{}

type cicdRunRuntimeConfiguration struct {
	Targets []cicdRunTargetConfiguration `json:"targets"`
}

type cicdRunTargetConfiguration struct {
	Provider string `json:"provider"`
	ScopeID  string `json:"scope_id"`
}

// PlanCICDRunWork returns one run and one work item per configured provider
// CI/CD run target.
func (p CICDRunWorkPlanner) PlanCICDRunWork(
	_ context.Context,
	request CICDRunPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validateCICDRunPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	targets, err := parseCICDRunRuntimeTargets(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	if err := validateUniqueCICDRunTargets(targets); err != nil {
		return workflow.Run{}, nil, err
	}

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              cicdRunRunID(request.Instance, request.PlanKey),
		TriggerKind:        cicdRunTriggerKind(request.Instance),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  cicdRunRequestedScopeSet(request.Instance, targets),
		RequestedCollector: string(scope.CollectorCICDRun),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for _, target := range targets {
		item, err := cicdRunWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validateCICDRunPlanRequest(request CICDRunPlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("ci/cd run plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorCICDRun {
		return fmt.Errorf("ci/cd run planner requires collector_kind %q", scope.CollectorCICDRun)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("ci/cd run planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("ci/cd run planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("ci/cd run planner observed_at must not be zero")
	}
	if err := validateSafePlanKey("ci/cd run planner", request.PlanKey); err != nil {
		return err
	}
	return nil
}

func parseCICDRunRuntimeTargets(raw string) ([]cicdRunTargetConfiguration, error) {
	if err := workflow.ValidateCICDRunCollectorConfiguration(raw); err != nil {
		return nil, err
	}
	var decoded cicdRunRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("decode ci/cd run collector configuration: %w", err)
	}
	targets := make([]cicdRunTargetConfiguration, 0, len(decoded.Targets))
	for _, target := range decoded.Targets {
		target.Provider = strings.TrimSpace(target.Provider)
		target.ScopeID = strings.TrimSpace(target.ScopeID)
		targets = append(targets, target)
	}
	return targets, nil
}

func validateUniqueCICDRunTargets(targets []cicdRunTargetConfiguration) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		scopeID := strings.TrimSpace(target.ScopeID)
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate ci/cd run target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

func cicdRunRunID(instance workflow.CollectorInstance, planKey string) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorCICDRun,
		strings.TrimSpace(instance.InstanceID),
		cicdRunTriggerKind(instance),
		strings.TrimSpace(planKey),
	)
}

func cicdRunTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

func cicdRunRequestedScopeSet(instance workflow.CollectorInstance, targets []cicdRunTargetConfiguration) string {
	type requestedTarget struct {
		ScopeID  string `json:"scope_id"`
		Provider string `json:"provider"`
	}
	payload := struct {
		CollectorInstanceID string            `json:"collector_instance_id"`
		Targets             []requestedTarget `json:"targets"`
	}{
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		Targets:             make([]requestedTarget, 0, len(targets)),
	}
	for _, target := range targets {
		payload.Targets = append(payload.Targets, requestedTarget{
			ScopeID:  strings.TrimSpace(target.ScopeID),
			Provider: strings.TrimSpace(target.Provider),
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

func cicdRunWorkItem(
	instance workflow.CollectorInstance,
	target cicdRunTargetConfiguration,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	scopeID := strings.TrimSpace(target.ScopeID)
	provider := strings.TrimSpace(target.Provider)
	generationID := "ci_cd_run:" + facts.StableID("CICDRunWorkflowGeneration", map[string]any{
		"instance_id": strings.TrimSpace(instance.InstanceID),
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    scopeID,
	})
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorCICDRun, instance.InstanceID, generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        string(scope.CollectorCICDRun),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorCICDRun, strings.TrimSpace(instance.InstanceID), provider),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}
