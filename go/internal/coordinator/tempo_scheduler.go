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

// TempoPlanRequest carries one Tempo trace-signal evidence planning request.
type TempoPlanRequest struct {
	// Instance is the durable Tempo collector instance to plan work for.
	Instance workflow.CollectorInstance
	// ObservedAt anchors the run and work-item timestamps for this reconcile.
	ObservedAt time.Time
	// PlanKey is the deterministic per-reconcile key that makes repeated
	// planning for the same instance and observation idempotent.
	PlanKey string
	// TriggerKind optionally overrides the derived schedule/bootstrap trigger.
	TriggerKind workflow.TriggerKind
	// ScopeIDs optionally restricts planning to a subset of configured targets.
	ScopeIDs []string
}

// TempoWorkPlanner plans workflow rows for configured Tempo targets without
// resolving credentials or contacting Tempo.
type TempoWorkPlanner struct{}

type tempoRuntimeConfiguration struct {
	Targets []tempoTargetConfiguration `json:"targets"`
}

type tempoTargetConfiguration struct {
	ScopeID    string `json:"scope_id"`
	InstanceID string `json:"instance_id"`
	BaseURL    string `json:"base_url"`
	Enabled    bool   `json:"enabled"`
}

// PlanTempoWork returns one run and one work item per enabled configured Tempo
// target. Disabled targets, an empty target list, and targets filtered out by
// ScopeIDs produce no work item. The returned plan is deterministic for a fixed
// (instance, plan key) pair so repeated reconciles are idempotent.
func (p TempoWorkPlanner) PlanTempoWork(
	_ context.Context,
	request TempoPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validateTempoPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	targets, err := parseTempoRuntimeTargets(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	if err := validateUniqueTempoTargets(targets); err != nil {
		return workflow.Run{}, nil, err
	}
	targets = filterEnabledTempoTargets(targets)
	targets = filterTempoTargetsByScopeIDs(targets, request.ScopeIDs)

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              tempoRunID(request.Instance, request.PlanKey, tempoRequestTriggerKind(request)),
		TriggerKind:        tempoRequestTriggerKind(request),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  tempoRequestedScopeSet(request.Instance, targets),
		RequestedCollector: string(scope.CollectorTempo),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for _, target := range targets {
		item, err := tempoWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validateTempoPlanRequest(request TempoPlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("tempo plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorTempo {
		return fmt.Errorf("tempo planner requires collector_kind %q", scope.CollectorTempo)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("tempo planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("tempo planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("tempo planner observed_at must not be zero")
	}
	if err := validateSafePlanKey("tempo planner", request.PlanKey); err != nil {
		return err
	}
	if request.TriggerKind != "" {
		if err := request.TriggerKind.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func parseTempoRuntimeTargets(raw string) ([]tempoTargetConfiguration, error) {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		normalized = "{}"
	}
	var decoded tempoRuntimeConfiguration
	if err := json.Unmarshal([]byte(normalized), &decoded); err != nil {
		return nil, fmt.Errorf("decode tempo collector configuration: %w", err)
	}
	targets := make([]tempoTargetConfiguration, 0, len(decoded.Targets))
	for i, target := range decoded.Targets {
		target.ScopeID = strings.TrimSpace(target.ScopeID)
		target.InstanceID = strings.TrimSpace(target.InstanceID)
		target.BaseURL = strings.TrimSpace(target.BaseURL)
		if target.ScopeID == "" {
			return nil, fmt.Errorf("tempo target[%d] requires scope_id", i)
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func validateUniqueTempoTargets(targets []tempoTargetConfiguration) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		scopeID := strings.TrimSpace(target.ScopeID)
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate tempo target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

func filterEnabledTempoTargets(targets []tempoTargetConfiguration) []tempoTargetConfiguration {
	out := make([]tempoTargetConfiguration, 0, len(targets))
	for _, target := range targets {
		if target.Enabled {
			out = append(out, target)
		}
	}
	return out
}

func tempoRunID(instance workflow.CollectorInstance, planKey string, triggerKind workflow.TriggerKind) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorTempo,
		strings.TrimSpace(instance.InstanceID),
		triggerKind,
		strings.TrimSpace(planKey),
	)
}

func tempoRequestTriggerKind(request TempoPlanRequest) workflow.TriggerKind {
	if request.TriggerKind != "" {
		return request.TriggerKind
	}
	return tempoTriggerKind(request.Instance)
}

func tempoTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

func filterTempoTargetsByScopeIDs(
	targets []tempoTargetConfiguration,
	scopeIDs []string,
) []tempoTargetConfiguration {
	if len(scopeIDs) == 0 {
		return targets
	}
	allowed := make(map[string]struct{}, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		scopeID = strings.TrimSpace(scopeID)
		if scopeID != "" {
			allowed[scopeID] = struct{}{}
		}
	}
	out := make([]tempoTargetConfiguration, 0, len(targets))
	for _, target := range targets {
		if _, ok := allowed[strings.TrimSpace(target.ScopeID)]; ok {
			out = append(out, target)
		}
	}
	return out
}

func tempoRequestedScopeSet(
	instance workflow.CollectorInstance,
	targets []tempoTargetConfiguration,
) string {
	type requestedTarget struct {
		ScopeID    string `json:"scope_id"`
		InstanceID string `json:"instance_id"`
		BaseURL    string `json:"base_url"`
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
			ScopeID:    strings.TrimSpace(target.ScopeID),
			InstanceID: strings.TrimSpace(target.InstanceID),
			BaseURL:    strings.TrimSpace(target.BaseURL),
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

func tempoWorkItem(
	instance workflow.CollectorInstance,
	target tempoTargetConfiguration,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	scopeID := strings.TrimSpace(target.ScopeID)
	generationID := "tempo:" + facts.StableID("TempoWorkflowGeneration", map[string]any{
		"instance_id": strings.TrimSpace(instance.InstanceID),
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    scopeID,
	})
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorTempo, instance.InstanceID, generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorTempo,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        string(scope.CollectorTempo),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorTempo, strings.TrimSpace(instance.InstanceID), scopeID),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}
