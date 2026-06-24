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

// PagerDutyPlanRequest carries one PagerDuty incident evidence planning request.
type PagerDutyPlanRequest struct {
	Instance    workflow.CollectorInstance
	ObservedAt  time.Time
	PlanKey     string
	TriggerKind workflow.TriggerKind
	ScopeIDs    []string
}

// PagerDutyWorkPlanner plans workflow rows for configured PagerDuty targets
// without resolving credentials or contacting PagerDuty.
type PagerDutyWorkPlanner struct{}

type pagerDutyRuntimeConfiguration struct {
	Targets []pagerDutyTargetConfiguration `json:"targets"`
}

type pagerDutyTargetConfiguration struct {
	Provider  string `json:"provider"`
	ScopeID   string `json:"scope_id"`
	AccountID string `json:"account_id"`
}

// PlanPagerDutyWork returns one run and one work item per configured PagerDuty
// target.
func (p PagerDutyWorkPlanner) PlanPagerDutyWork(
	_ context.Context,
	request PagerDutyPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validatePagerDutyPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	targets, err := parsePagerDutyRuntimeTargets(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	if err := validateUniquePagerDutyTargets(targets); err != nil {
		return workflow.Run{}, nil, err
	}
	targets = filterPagerDutyTargetsByScopeIDs(targets, request.ScopeIDs)

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              pagerDutyRunID(request.Instance, request.PlanKey, pagerDutyRequestTriggerKind(request)),
		TriggerKind:        pagerDutyRequestTriggerKind(request),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  pagerDutyRequestedScopeSet(request.Instance, targets),
		RequestedCollector: string(scope.CollectorPagerDuty),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for _, target := range targets {
		item, err := pagerDutyWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validatePagerDutyPlanRequest(request PagerDutyPlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("pagerduty plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorPagerDuty {
		return fmt.Errorf("pagerduty planner requires collector_kind %q", scope.CollectorPagerDuty)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("pagerduty planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("pagerduty planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("pagerduty planner observed_at must not be zero")
	}
	if err := validateSafePlanKey("pagerduty planner", request.PlanKey); err != nil {
		return err
	}
	if request.TriggerKind != "" {
		if err := request.TriggerKind.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func parsePagerDutyRuntimeTargets(raw string) ([]pagerDutyTargetConfiguration, error) {
	if err := workflow.ValidatePagerDutyCollectorConfiguration(raw); err != nil {
		return nil, err
	}
	var decoded pagerDutyRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("decode pagerduty collector configuration: %w", err)
	}
	targets := make([]pagerDutyTargetConfiguration, 0, len(decoded.Targets))
	for _, target := range decoded.Targets {
		target.Provider = strings.TrimSpace(target.Provider)
		target.ScopeID = strings.TrimSpace(target.ScopeID)
		target.AccountID = strings.TrimSpace(target.AccountID)
		targets = append(targets, target)
	}
	return targets, nil
}

func validateUniquePagerDutyTargets(targets []pagerDutyTargetConfiguration) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		scopeID := strings.TrimSpace(target.ScopeID)
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate pagerduty target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

func pagerDutyRunID(instance workflow.CollectorInstance, planKey string, triggerKind workflow.TriggerKind) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorPagerDuty,
		strings.TrimSpace(instance.InstanceID),
		triggerKind,
		strings.TrimSpace(planKey),
	)
}

func pagerDutyRequestTriggerKind(request PagerDutyPlanRequest) workflow.TriggerKind {
	if request.TriggerKind != "" {
		return request.TriggerKind
	}
	return pagerDutyTriggerKind(request.Instance)
}

func pagerDutyTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

func filterPagerDutyTargetsByScopeIDs(
	targets []pagerDutyTargetConfiguration,
	scopeIDs []string,
) []pagerDutyTargetConfiguration {
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
	out := make([]pagerDutyTargetConfiguration, 0, len(targets))
	for _, target := range targets {
		if _, ok := allowed[strings.TrimSpace(target.ScopeID)]; ok {
			out = append(out, target)
		}
	}
	return out
}

func pagerDutyRequestedScopeSet(
	instance workflow.CollectorInstance,
	targets []pagerDutyTargetConfiguration,
) string {
	type requestedTarget struct {
		ScopeID   string `json:"scope_id"`
		Provider  string `json:"provider"`
		AccountID string `json:"account_id"`
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
			ScopeID:   strings.TrimSpace(target.ScopeID),
			Provider:  strings.TrimSpace(target.Provider),
			AccountID: strings.TrimSpace(target.AccountID),
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

func pagerDutyWorkItem(
	instance workflow.CollectorInstance,
	target pagerDutyTargetConfiguration,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	scopeID := strings.TrimSpace(target.ScopeID)
	provider := strings.TrimSpace(target.Provider)
	generationID := "pagerduty:" + facts.StableID("PagerDutyWorkflowGeneration", map[string]any{
		"instance_id": strings.TrimSpace(instance.InstanceID),
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    scopeID,
	})
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorPagerDuty, instance.InstanceID, generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorPagerDuty,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        string(scope.CollectorPagerDuty),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorPagerDuty, strings.TrimSpace(instance.InstanceID), provider),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}
