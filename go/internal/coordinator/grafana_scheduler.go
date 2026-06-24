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

// GrafanaPlanRequest carries one Grafana observability planning request. The
// coordinator supplies the resolved collector instance, the reconcile observed
// time, a deterministic plan key, and optional scope and trigger overrides for
// freshness wake-ups; the planner never resolves credentials or contacts
// Grafana.
type GrafanaPlanRequest struct {
	Instance    workflow.CollectorInstance
	ObservedAt  time.Time
	PlanKey     string
	TriggerKind workflow.TriggerKind
	ScopeIDs    []string
}

// GrafanaWorkPlanner plans workflow rows for enabled Grafana targets without
// resolving credentials or contacting Grafana. It is the concrete planner the
// coordinator wires for the grafana collector kind.
type GrafanaWorkPlanner struct{}

type grafanaRuntimeConfiguration struct {
	Targets []grafanaTargetConfiguration `json:"targets"`
}

type grafanaTargetConfiguration struct {
	Provider   string `json:"provider"`
	ScopeID    string `json:"scope_id"`
	InstanceID string `json:"instance_id"`
	Enabled    bool   `json:"enabled"`
}

// PlanGrafanaWork returns one collection-pending run and one work item per
// enabled, scope-allowed Grafana target. Disabled targets are skipped, so an
// instance with no enabled targets yields no work. The run, work item, and
// generation identities are deterministic for a fixed instance and plan key,
// which keeps repeated reconciles idempotent.
func (p GrafanaWorkPlanner) PlanGrafanaWork(
	_ context.Context,
	request GrafanaPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validateGrafanaPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	targets, err := parseGrafanaRuntimeTargets(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	if err := validateUniqueGrafanaTargets(targets); err != nil {
		return workflow.Run{}, nil, err
	}
	targets = filterEnabledGrafanaTargets(targets)
	targets = filterGrafanaTargetsByScopeIDs(targets, request.ScopeIDs)

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              grafanaRunID(request.Instance, request.PlanKey, grafanaRequestTriggerKind(request)),
		TriggerKind:        grafanaRequestTriggerKind(request),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  grafanaRequestedScopeSet(request.Instance, targets),
		RequestedCollector: string(scope.CollectorGrafana),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for _, target := range targets {
		item, err := grafanaWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validateGrafanaPlanRequest(request GrafanaPlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("grafana plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorGrafana {
		return fmt.Errorf("grafana planner requires collector_kind %q", scope.CollectorGrafana)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("grafana planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("grafana planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("grafana planner observed_at must not be zero")
	}
	if err := validateSafePlanKey("grafana planner", request.PlanKey); err != nil {
		return err
	}
	if request.TriggerKind != "" {
		if err := request.TriggerKind.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func parseGrafanaRuntimeTargets(raw string) ([]grafanaTargetConfiguration, error) {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		normalized = "{}"
	}
	var decoded grafanaRuntimeConfiguration
	if err := json.Unmarshal([]byte(normalized), &decoded); err != nil {
		return nil, fmt.Errorf("decode grafana collector configuration: %w", err)
	}
	targets := make([]grafanaTargetConfiguration, 0, len(decoded.Targets))
	for _, target := range decoded.Targets {
		target.Provider = strings.TrimSpace(target.Provider)
		target.ScopeID = strings.TrimSpace(target.ScopeID)
		target.InstanceID = strings.TrimSpace(target.InstanceID)
		targets = append(targets, target)
	}
	return targets, nil
}

func validateUniqueGrafanaTargets(targets []grafanaTargetConfiguration) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		scopeID := strings.TrimSpace(target.ScopeID)
		if scopeID == "" {
			return fmt.Errorf("grafana target scope_id must not be blank")
		}
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate grafana target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

func filterEnabledGrafanaTargets(targets []grafanaTargetConfiguration) []grafanaTargetConfiguration {
	out := make([]grafanaTargetConfiguration, 0, len(targets))
	for _, target := range targets {
		if target.Enabled {
			out = append(out, target)
		}
	}
	return out
}

func grafanaRunID(instance workflow.CollectorInstance, planKey string, triggerKind workflow.TriggerKind) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorGrafana,
		strings.TrimSpace(instance.InstanceID),
		triggerKind,
		strings.TrimSpace(planKey),
	)
}

func grafanaRequestTriggerKind(request GrafanaPlanRequest) workflow.TriggerKind {
	if request.TriggerKind != "" {
		return request.TriggerKind
	}
	return grafanaTriggerKind(request.Instance)
}

func grafanaTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

func filterGrafanaTargetsByScopeIDs(
	targets []grafanaTargetConfiguration,
	scopeIDs []string,
) []grafanaTargetConfiguration {
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
	out := make([]grafanaTargetConfiguration, 0, len(targets))
	for _, target := range targets {
		if _, ok := allowed[strings.TrimSpace(target.ScopeID)]; ok {
			out = append(out, target)
		}
	}
	return out
}

func grafanaRequestedScopeSet(
	instance workflow.CollectorInstance,
	targets []grafanaTargetConfiguration,
) string {
	type requestedTarget struct {
		ScopeID    string `json:"scope_id"`
		Provider   string `json:"provider"`
		InstanceID string `json:"instance_id"`
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

func grafanaTargetConflictKey(target grafanaTargetConfiguration) string {
	if instanceID := strings.TrimSpace(target.InstanceID); instanceID != "" {
		return instanceID
	}
	return strings.TrimSpace(target.ScopeID)
}

func grafanaWorkItem(
	instance workflow.CollectorInstance,
	target grafanaTargetConfiguration,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	scopeID := strings.TrimSpace(target.ScopeID)
	conflictKey := grafanaTargetConflictKey(target)
	generationID := "grafana:" + facts.StableID("GrafanaWorkflowGeneration", map[string]any{
		"instance_id": strings.TrimSpace(instance.InstanceID),
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    scopeID,
	})
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorGrafana, strings.TrimSpace(instance.InstanceID), generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorGrafana,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        string(scope.CollectorGrafana),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorGrafana, strings.TrimSpace(instance.InstanceID), conflictKey),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}
