// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/contract"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// PlanRequest carries one hosted provider security-alert planning request.
type PlanRequest struct {
	Instance   workflow.CollectorInstance
	ObservedAt time.Time
	PlanKey    string
}

// WorkPlanner plans workflow rows for configured provider
// security-alert targets without resolving credentials or contacting providers.
type WorkPlanner struct {
	// ShardScopes reads the scopes org targets already have, so the planner
	// can retire the ones the current partition no longer plans (#9019). It
	// is required when the configuration has an org target; a
	// repository-only configuration never calls it.
	ShardScopes ShardScopeReader
	// Logger receives the retirement summary and the per-scan bound warning.
	// Nil disables them.
	Logger *slog.Logger
}

type securityAlertRuntimeConfiguration struct {
	Targets []securityAlertTargetConfiguration `json:"targets"`
}

type securityAlertTargetConfiguration struct {
	Provider            string   `json:"provider"`
	Scope               string   `json:"scope"`
	ScopeID             string   `json:"scope_id"`
	Organization        string   `json:"organization"`
	AllowedRepositories []string `json:"allowed_repositories"`
}

// PlanSecurityAlertWork returns one run and one work item per planned scope:
// a repository target is one scope; an org target is one scope per shard of
// its partition (shard.Partition), the base scope_id when it has one shard.
// Each scope of an org target that still has a non-retired active generation
// but is no longer planned gets one retirement work item, marked by
// shard.RetirementAcceptanceUnitID, which the collector answers with an empty
// generation.
func (p WorkPlanner) PlanSecurityAlertWork(
	ctx context.Context,
	request PlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validateSecurityAlertPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	targets, err := parseSecurityAlertRuntimeTargets(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	if err := validateUniqueSecurityAlertTargets(targets); err != nil {
		return workflow.Run{}, nil, err
	}
	scopes, err := p.plannedScopes(ctx, request.Instance, targets)
	if err != nil {
		return workflow.Run{}, nil, err
	}

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              securityAlertRunID(request.Instance, request.PlanKey),
		TriggerKind:        securityAlertTriggerKind(request.Instance),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  securityAlertRequestedScopeSet(request.Instance, scopes),
		RequestedCollector: string(scope.CollectorSecurityAlert),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(scopes))
	for _, planned := range scopes {
		item, err := securityAlertWorkItem(request.Instance, planned, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validateSecurityAlertPlanRequest(request PlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("security alert plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorSecurityAlert {
		return fmt.Errorf("security alert planner requires collector_kind %q", scope.CollectorSecurityAlert)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("security alert planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("security alert planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("security alert planner observed_at must not be zero")
	}
	if err := contract.ValidateSafePlanKey("security alert planner", request.PlanKey); err != nil {
		return err
	}
	return nil
}

func parseSecurityAlertRuntimeTargets(raw string) ([]securityAlertTargetConfiguration, error) {
	if err := workflow.ValidateSecurityAlertCollectorConfiguration(raw); err != nil {
		return nil, err
	}
	var decoded securityAlertRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("decode security alert collector configuration: %w", err)
	}
	targets := make([]securityAlertTargetConfiguration, 0, len(decoded.Targets))
	for _, target := range decoded.Targets {
		target.Provider = strings.TrimSpace(target.Provider)
		target.Scope = strings.TrimSpace(target.Scope)
		target.ScopeID = strings.TrimSpace(target.ScopeID)
		targets = append(targets, target)
	}
	return targets, nil
}

func validateUniqueSecurityAlertTargets(targets []securityAlertTargetConfiguration) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		scopeID := strings.TrimSpace(target.ScopeID)
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate security alert target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

func securityAlertRunID(instance workflow.CollectorInstance, planKey string) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorSecurityAlert,
		strings.TrimSpace(instance.InstanceID),
		securityAlertTriggerKind(instance),
		strings.TrimSpace(planKey),
	)
}

func securityAlertTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

func securityAlertRequestedScopeSet(
	instance workflow.CollectorInstance,
	scopes []plannedScope,
) string {
	type requestedTarget struct {
		ScopeID  string `json:"scope_id"`
		Provider string `json:"provider"`
		// Retire marks a retirement item; omitted for collection, so a
		// configuration without org shards keeps its exact prior payload.
		Retire bool `json:"retire,omitempty"`
	}
	payload := struct {
		CollectorInstanceID string            `json:"collector_instance_id"`
		Targets             []requestedTarget `json:"targets"`
	}{
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		Targets:             make([]requestedTarget, 0, len(scopes)),
	}
	for _, planned := range scopes {
		payload.Targets = append(payload.Targets, requestedTarget{
			ScopeID:  planned.scopeID,
			Provider: planned.provider,
			Retire:   planned.retire,
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

func securityAlertWorkItem(
	instance workflow.CollectorInstance,
	planned plannedScope,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	scopeID := planned.scopeID
	provider := planned.provider
	identity := map[string]any{
		"instance_id": strings.TrimSpace(instance.InstanceID),
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    scopeID,
	}
	acceptanceUnitID := scopeID
	if planned.retire {
		// A retirement never shares a generation id with a collection of
		// the same scope and plan key; a collection keeps its prior identity.
		identity["retire"] = true
		acceptanceUnitID = shard.RetirementAcceptanceUnitID(scopeID)
	}
	generationID := "security_alert:" + facts.StableID("SecurityAlertWorkflowGeneration", identity)
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorSecurityAlert, instance.InstanceID, generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorSecurityAlert,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        string(scope.CollectorSecurityAlert),
		ScopeID:             scopeID,
		AcceptanceUnitID:    acceptanceUnitID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorSecurityAlert, strings.TrimSpace(instance.InstanceID), provider),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}
