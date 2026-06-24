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

// PrometheusMimirPlanRequest carries one Prometheus/Mimir metric-metadata work
// planning request. ScopeIDs, when set, restricts planning to the named target
// scopes so a targeted refresh can re-poll a subset of configured sources.
type PrometheusMimirPlanRequest struct {
	Instance    workflow.CollectorInstance
	ObservedAt  time.Time
	PlanKey     string
	TriggerKind workflow.TriggerKind
	ScopeIDs    []string
}

// PrometheusMimirWorkPlanner plans workflow rows for configured Prometheus and
// Grafana Mimir metric targets without resolving credentials or contacting any
// provider. It mirrors the periodic external-API poll planners (Jira, PagerDuty,
// scheduled AWS): one bounded work item per enabled target tuple.
type PrometheusMimirWorkPlanner struct{}

type prometheusMimirRuntimeConfiguration struct {
	Targets []prometheusMimirTargetConfiguration `json:"targets"`
}

// prometheusMimirTargetConfiguration mirrors the per-target shape parsed by the
// collector config loader at go/cmd/collector-prometheus-mimir/config.go. Only
// the fields needed for scheduling and provenance are retained; credential env
// names are intentionally omitted so they never reach durable run metadata.
type prometheusMimirTargetConfiguration struct {
	Provider   string `json:"provider"`
	ScopeID    string `json:"scope_id"`
	InstanceID string `json:"instance_id"`
	TenantID   string `json:"tenant_id"`
	Enabled    bool   `json:"enabled"`
}

// PlanPrometheusMimirWork returns one run and one work item per enabled metric
// target parsed from the collector instance configuration. Disabled targets,
// empty configuration, and an empty target list all yield no work items. The
// per-target conflict domain is the target scope_id (carried as ScopeID and
// AcceptanceUnitID), and the per-target fairness lane is keyed on the same
// scope so no two concurrent claims contend for one metric source.
func (p PrometheusMimirWorkPlanner) PlanPrometheusMimirWork(
	_ context.Context,
	request PrometheusMimirPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validatePrometheusMimirPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	targets, err := parsePrometheusMimirRuntimeTargets(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	targets = enabledPrometheusMimirTargets(targets)
	if err := validateUniquePrometheusMimirTargets(targets); err != nil {
		return workflow.Run{}, nil, err
	}
	targets = filterPrometheusMimirTargetsByScopeIDs(targets, request.ScopeIDs)

	observedAt := request.ObservedAt.UTC()
	triggerKind := prometheusMimirRequestTriggerKind(request)
	run := workflow.Run{
		RunID:              prometheusMimirRunID(request.Instance, request.PlanKey, triggerKind),
		TriggerKind:        triggerKind,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  prometheusMimirRequestedScopeSet(request.Instance, targets),
		RequestedCollector: string(scope.CollectorPrometheusMimir),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for _, target := range targets {
		item, err := prometheusMimirWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validatePrometheusMimirPlanRequest(request PrometheusMimirPlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("prometheus/mimir plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorPrometheusMimir {
		return fmt.Errorf("prometheus/mimir planner requires collector_kind %q", scope.CollectorPrometheusMimir)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("prometheus/mimir planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("prometheus/mimir planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("prometheus/mimir planner observed_at must not be zero")
	}
	if err := validateSafePlanKey("prometheus/mimir planner", request.PlanKey); err != nil {
		return err
	}
	if request.TriggerKind != "" {
		if err := request.TriggerKind.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func parsePrometheusMimirRuntimeTargets(raw string) ([]prometheusMimirTargetConfiguration, error) {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		normalized = "{}"
	}
	var decoded prometheusMimirRuntimeConfiguration
	if err := json.Unmarshal([]byte(normalized), &decoded); err != nil {
		return nil, fmt.Errorf("decode prometheus/mimir collector configuration: %w", err)
	}
	targets := make([]prometheusMimirTargetConfiguration, 0, len(decoded.Targets))
	for _, target := range decoded.Targets {
		target.Provider = strings.TrimSpace(target.Provider)
		target.ScopeID = strings.TrimSpace(target.ScopeID)
		target.InstanceID = strings.TrimSpace(target.InstanceID)
		target.TenantID = strings.TrimSpace(target.TenantID)
		targets = append(targets, target)
	}
	return targets, nil
}

// enabledPrometheusMimirTargets drops disabled targets so they never schedule
// work while the same instance keeps polling its enabled siblings.
func enabledPrometheusMimirTargets(
	targets []prometheusMimirTargetConfiguration,
) []prometheusMimirTargetConfiguration {
	out := make([]prometheusMimirTargetConfiguration, 0, len(targets))
	for _, target := range targets {
		if target.Enabled {
			out = append(out, target)
		}
	}
	return out
}

func validateUniquePrometheusMimirTargets(targets []prometheusMimirTargetConfiguration) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		scopeID := strings.TrimSpace(target.ScopeID)
		if scopeID == "" {
			return fmt.Errorf("prometheus/mimir target scope_id must not be blank")
		}
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate prometheus/mimir target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

func prometheusMimirRunID(
	instance workflow.CollectorInstance,
	planKey string,
	triggerKind workflow.TriggerKind,
) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorPrometheusMimir,
		strings.TrimSpace(instance.InstanceID),
		triggerKind,
		strings.TrimSpace(planKey),
	)
}

func prometheusMimirRequestTriggerKind(request PrometheusMimirPlanRequest) workflow.TriggerKind {
	if request.TriggerKind != "" {
		return request.TriggerKind
	}
	return prometheusMimirTriggerKind(request.Instance)
}

func prometheusMimirTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

func filterPrometheusMimirTargetsByScopeIDs(
	targets []prometheusMimirTargetConfiguration,
	scopeIDs []string,
) []prometheusMimirTargetConfiguration {
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
	out := make([]prometheusMimirTargetConfiguration, 0, len(targets))
	for _, target := range targets {
		if _, ok := allowed[strings.TrimSpace(target.ScopeID)]; ok {
			out = append(out, target)
		}
	}
	return out
}

func prometheusMimirRequestedScopeSet(
	instance workflow.CollectorInstance,
	targets []prometheusMimirTargetConfiguration,
) string {
	type requestedTarget struct {
		ScopeID    string `json:"scope_id"`
		Provider   string `json:"provider"`
		InstanceID string `json:"instance_id"`
		TenantID   string `json:"tenant_id,omitempty"`
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
			Provider:   strings.TrimSpace(target.Provider),
			InstanceID: strings.TrimSpace(target.InstanceID),
			TenantID:   strings.TrimSpace(target.TenantID),
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

func prometheusMimirWorkItem(
	instance workflow.CollectorInstance,
	target prometheusMimirTargetConfiguration,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	scopeID := strings.TrimSpace(target.ScopeID)
	generationID := "prometheus_mimir:" + facts.StableID("PrometheusMimirWorkflowGeneration", map[string]any{
		"instance_id": strings.TrimSpace(instance.InstanceID),
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    scopeID,
	})
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorPrometheusMimir, strings.TrimSpace(instance.InstanceID), generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorPrometheusMimir,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        string(scope.CollectorPrometheusMimir),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorPrometheusMimir, strings.TrimSpace(instance.InstanceID), scopeID),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}
