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

// JiraPlanRequest carries one Jira work-item evidence planning request.
type JiraPlanRequest struct {
	Instance    workflow.CollectorInstance
	ObservedAt  time.Time
	PlanKey     string
	TriggerKind workflow.TriggerKind
	ScopeIDs    []string
}

// JiraWorkPlanner plans workflow rows for configured Jira targets without
// resolving credentials or contacting Jira.
type JiraWorkPlanner struct{}

type jiraRuntimeConfiguration struct {
	Targets []jiraTargetConfiguration `json:"targets"`
}

type jiraTargetConfiguration struct {
	Provider string `json:"provider"`
	ScopeID  string `json:"scope_id"`
	SiteID   string `json:"site_id"`
}

// PlanJiraWork returns one run and one work item per configured Jira target.
func (p JiraWorkPlanner) PlanJiraWork(
	_ context.Context,
	request JiraPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validateJiraPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	targets, err := parseJiraRuntimeTargets(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	if err := validateUniqueJiraTargets(targets); err != nil {
		return workflow.Run{}, nil, err
	}
	targets = filterJiraTargetsByScopeIDs(targets, request.ScopeIDs)

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              jiraRunID(request.Instance, request.PlanKey, jiraRequestTriggerKind(request)),
		TriggerKind:        jiraRequestTriggerKind(request),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  jiraRequestedScopeSet(request.Instance, targets),
		RequestedCollector: string(scope.CollectorJira),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for _, target := range targets {
		item, err := jiraWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validateJiraPlanRequest(request JiraPlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("jira plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorJira {
		return fmt.Errorf("jira planner requires collector_kind %q", scope.CollectorJira)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("jira planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("jira planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("jira planner observed_at must not be zero")
	}
	if err := validateSafePlanKey("jira planner", request.PlanKey); err != nil {
		return err
	}
	if request.TriggerKind != "" {
		if err := request.TriggerKind.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func parseJiraRuntimeTargets(raw string) ([]jiraTargetConfiguration, error) {
	if err := workflow.ValidateJiraCollectorConfiguration(raw); err != nil {
		return nil, err
	}
	var decoded jiraRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("decode jira collector configuration: %w", err)
	}
	targets := make([]jiraTargetConfiguration, 0, len(decoded.Targets))
	for _, target := range decoded.Targets {
		target.Provider = strings.TrimSpace(target.Provider)
		target.ScopeID = strings.TrimSpace(target.ScopeID)
		target.SiteID = strings.TrimSpace(target.SiteID)
		targets = append(targets, target)
	}
	return targets, nil
}

func validateUniqueJiraTargets(targets []jiraTargetConfiguration) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		scopeID := strings.TrimSpace(target.ScopeID)
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate jira target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

func jiraRunID(instance workflow.CollectorInstance, planKey string, triggerKind workflow.TriggerKind) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorJira,
		strings.TrimSpace(instance.InstanceID),
		triggerKind,
		strings.TrimSpace(planKey),
	)
}

func jiraRequestTriggerKind(request JiraPlanRequest) workflow.TriggerKind {
	if request.TriggerKind != "" {
		return request.TriggerKind
	}
	return jiraTriggerKind(request.Instance)
}

func jiraTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

func filterJiraTargetsByScopeIDs(
	targets []jiraTargetConfiguration,
	scopeIDs []string,
) []jiraTargetConfiguration {
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
	out := make([]jiraTargetConfiguration, 0, len(targets))
	for _, target := range targets {
		if _, ok := allowed[strings.TrimSpace(target.ScopeID)]; ok {
			out = append(out, target)
		}
	}
	return out
}

func jiraRequestedScopeSet(
	instance workflow.CollectorInstance,
	targets []jiraTargetConfiguration,
) string {
	type requestedTarget struct {
		ScopeID  string `json:"scope_id"`
		Provider string `json:"provider"`
		SiteID   string `json:"site_id"`
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
			SiteID:   strings.TrimSpace(target.SiteID),
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

func jiraWorkItem(
	instance workflow.CollectorInstance,
	target jiraTargetConfiguration,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	scopeID := strings.TrimSpace(target.ScopeID)
	siteID := strings.TrimSpace(target.SiteID)
	generationID := "jira:" + facts.StableID("JiraWorkflowGeneration", map[string]any{
		"instance_id": strings.TrimSpace(instance.InstanceID),
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    scopeID,
	})
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorJira, instance.InstanceID, generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorJira,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        string(scope.CollectorJira),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorJira, strings.TrimSpace(instance.InstanceID), siteID),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}
