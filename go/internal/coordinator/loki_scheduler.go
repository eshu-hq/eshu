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

// LokiPlanRequest carries one Loki observability work-item planning request.
type LokiPlanRequest struct {
	// Instance is the durable Loki collector instance whose configuration the
	// planner reads to enumerate targets.
	Instance workflow.CollectorInstance
	// ObservedAt anchors the planned run and work-item timestamps. It must not
	// be zero.
	ObservedAt time.Time
	// PlanKey makes the planned run and work items deterministic for one
	// scheduling tick; the same (instance, plan key) always yields the same IDs.
	PlanKey string
	// TriggerKind optionally overrides the derived schedule/bootstrap trigger.
	TriggerKind workflow.TriggerKind
	// ScopeIDs optionally restricts planning to a subset of configured targets.
	ScopeIDs []string
}

// LokiWorkPlanner plans workflow rows for configured Loki targets without
// resolving credentials or contacting Loki.
type LokiWorkPlanner struct{}

type lokiRuntimeConfiguration struct {
	Targets []lokiTargetConfiguration `json:"targets"`
}

type lokiTargetConfiguration struct {
	ScopeID    string `json:"scope_id"`
	InstanceID string `json:"instance_id"`
	BaseURL    string `json:"base_url"`
	Enabled    bool   `json:"enabled"`
}

// PlanLokiWork returns one scheduled run plus one work item per enabled Loki
// target parsed from the collector instance configuration. Disabled targets are
// skipped and never produce work. The returned run records the planned targets
// in requested_scope_set so an operator can audit what was scheduled.
func (p LokiWorkPlanner) PlanLokiWork(
	_ context.Context,
	request LokiPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validateLokiPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	targets, err := parseLokiRuntimeTargets(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	if err := validateUniqueLokiTargets(targets); err != nil {
		return workflow.Run{}, nil, err
	}
	targets = enabledLokiTargets(targets)
	targets = filterLokiTargetsByScopeIDs(targets, request.ScopeIDs)

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              lokiRunID(request.Instance, request.PlanKey, lokiRequestTriggerKind(request)),
		TriggerKind:        lokiRequestTriggerKind(request),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  lokiRequestedScopeSet(request.Instance, targets),
		RequestedCollector: string(scope.CollectorLoki),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for _, target := range targets {
		item, err := lokiWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validateLokiPlanRequest(request LokiPlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("loki plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorLoki {
		return fmt.Errorf("loki planner requires collector_kind %q", scope.CollectorLoki)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("loki planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("loki planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("loki planner observed_at must not be zero")
	}
	if err := validateSafePlanKey("loki planner", request.PlanKey); err != nil {
		return err
	}
	if request.TriggerKind != "" {
		if err := request.TriggerKind.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func parseLokiRuntimeTargets(raw string) ([]lokiTargetConfiguration, error) {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		normalized = "{}"
	}
	var decoded lokiRuntimeConfiguration
	if err := json.Unmarshal([]byte(normalized), &decoded); err != nil {
		return nil, fmt.Errorf("decode loki collector configuration: %w", err)
	}
	targets := make([]lokiTargetConfiguration, 0, len(decoded.Targets))
	for _, target := range decoded.Targets {
		target.ScopeID = strings.TrimSpace(target.ScopeID)
		target.InstanceID = strings.TrimSpace(target.InstanceID)
		target.BaseURL = strings.TrimSpace(target.BaseURL)
		targets = append(targets, target)
	}
	return targets, nil
}

func enabledLokiTargets(targets []lokiTargetConfiguration) []lokiTargetConfiguration {
	out := make([]lokiTargetConfiguration, 0, len(targets))
	for _, target := range targets {
		if target.Enabled {
			out = append(out, target)
		}
	}
	return out
}

func validateUniqueLokiTargets(targets []lokiTargetConfiguration) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if target.ScopeID == "" {
			return fmt.Errorf("loki target scope_id must not be blank")
		}
		if _, ok := seen[target.ScopeID]; ok {
			return fmt.Errorf("duplicate loki target scope_id %q", target.ScopeID)
		}
		seen[target.ScopeID] = struct{}{}
	}
	return nil
}

func lokiRunID(instance workflow.CollectorInstance, planKey string, triggerKind workflow.TriggerKind) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorLoki,
		strings.TrimSpace(instance.InstanceID),
		triggerKind,
		strings.TrimSpace(planKey),
	)
}

// lokiRequestTriggerKind honors an explicit request override and otherwise
// derives the trigger from the instance bootstrap flag.
func lokiRequestTriggerKind(request LokiPlanRequest) workflow.TriggerKind {
	if request.TriggerKind != "" {
		return request.TriggerKind
	}
	return lokiTriggerKind(request.Instance)
}

func lokiTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

// filterLokiTargetsByScopeIDs restricts planning to the requested scope IDs;
// an empty scopeIDs slice plans every enabled target.
func filterLokiTargetsByScopeIDs(
	targets []lokiTargetConfiguration,
	scopeIDs []string,
) []lokiTargetConfiguration {
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
	out := make([]lokiTargetConfiguration, 0, len(targets))
	for _, target := range targets {
		if _, ok := allowed[target.ScopeID]; ok {
			out = append(out, target)
		}
	}
	return out
}

func lokiRequestedScopeSet(
	instance workflow.CollectorInstance,
	targets []lokiTargetConfiguration,
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
			ScopeID:    target.ScopeID,
			InstanceID: target.InstanceID,
			BaseURL:    target.BaseURL,
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

func lokiWorkItem(
	instance workflow.CollectorInstance,
	target lokiTargetConfiguration,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	scopeID := target.ScopeID
	generationID := "loki:" + facts.StableID("LokiWorkflowGeneration", map[string]any{
		"instance_id": strings.TrimSpace(instance.InstanceID),
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    scopeID,
	})
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorLoki, strings.TrimSpace(instance.InstanceID), generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorLoki,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        string(scope.CollectorLoki),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		// FairnessKey partitions claims by the per-target Loki source so two
		// concurrent reconciles never plan overlapping work for the same target.
		FairnessKey: fmt.Sprintf("%s:%s:%s", scope.CollectorLoki, strings.TrimSpace(instance.InstanceID), scopeID),
		Status:      workflow.WorkItemStatusPending,
		CreatedAt:   observedAt.UTC(),
		UpdatedAt:   observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}
