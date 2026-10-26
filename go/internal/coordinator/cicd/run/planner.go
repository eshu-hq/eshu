// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package run

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/contract"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// PlanRequest carries one hosted provider CI/CD run planning request.
type PlanRequest struct {
	Instance   workflow.CollectorInstance
	ObservedAt time.Time
	PlanKey    string
	// TriggerKind selects a webhook wake-up; zero retains scheduled planning.
	TriggerKind workflow.TriggerKind
	// ScopeIDs limits webhook planning to configured GitHub Actions targets.
	ScopeIDs []string
	// ScheduledProvider isolates one configured provider's scheduled run when
	// a collector instance has mixed provider targets.
	ScheduledProvider string
}

// WorkPlanner plans workflow rows for configured CI/CD run targets
// without resolving credentials or contacting providers.
type WorkPlanner struct{}

type cicdRunRuntimeConfiguration struct {
	Targets []cicdRunTargetConfiguration `json:"targets"`
}

type cicdRunTargetConfiguration struct {
	Provider   string `json:"provider"`
	ScopeID    string `json:"scope_id"`
	Repository string `json:"repository"`
}

// PlanCICDRunWork returns one run and one work item per configured provider
// CI/CD run target.
func (p WorkPlanner) PlanCICDRunWork(
	_ context.Context,
	request PlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validatePlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	targets, err := parseCICDRunRuntimeTargets(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	if err := validateUniqueCICDRunTargets(targets); err != nil {
		return workflow.Run{}, nil, err
	}
	triggerKind := cicdRunTriggerKind(request.Instance)
	if request.TriggerKind != "" {
		triggerKind = request.TriggerKind
	}
	if triggerKind == workflow.TriggerKindWebhook {
		if request.ScheduledProvider != "" {
			return workflow.Run{}, nil, fmt.Errorf("ci/cd run scheduled_provider cannot select webhook targets")
		}
		targets, err = selectWebhookTargets(targets, request.ScopeIDs)
		if err != nil {
			return workflow.Run{}, nil, err
		}
	} else if len(request.ScopeIDs) != 0 {
		return workflow.Run{}, nil, fmt.Errorf("ci/cd run scope_ids require webhook trigger")
	} else if request.ScheduledProvider != "" {
		targets, err = selectScheduledProviderTargets(targets, request.ScheduledProvider)
		if err != nil {
			return workflow.Run{}, nil, err
		}
	}

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              cicdRunRunID(request.Instance, request.PlanKey, triggerKind),
		TriggerKind:        triggerKind,
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

func selectScheduledProviderTargets(targets []cicdRunTargetConfiguration, provider string) ([]cicdRunTargetConfiguration, error) {
	if provider != "github_actions" && provider != "gitlab_ci" {
		return nil, fmt.Errorf("ci/cd run scheduled provider %q is unsupported", provider)
	}
	selected := make([]cicdRunTargetConfiguration, 0, len(targets))
	for _, target := range targets {
		if target.Provider == provider {
			selected = append(selected, target)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("ci/cd run scheduled provider %q has no configured targets", provider)
	}
	return selected, nil
}

func selectWebhookTargets(targets []cicdRunTargetConfiguration, scopes []string) ([]cicdRunTargetConfiguration, error) {
	if len(scopes) == 0 {
		return nil, fmt.Errorf("ci/cd run webhook scope_ids are required")
	}
	requested := make(map[string]struct{}, len(scopes))
	for _, scopeID := range scopes {
		if strings.TrimSpace(scopeID) == "" {
			return nil, fmt.Errorf("ci/cd run webhook scope_id is required")
		}
		requested[strings.TrimSpace(scopeID)] = struct{}{}
	}
	selected := make([]cicdRunTargetConfiguration, 0, len(requested))
	for _, target := range targets {
		if _, ok := requested[target.ScopeID]; !ok {
			continue
		}
		if target.Provider != "github_actions" {
			return nil, fmt.Errorf("ci/cd run webhook scope_id %q requires github_actions provider", target.ScopeID)
		}
		selected = append(selected, target)
		delete(requested, target.ScopeID)
	}
	if len(requested) != 0 {
		return nil, fmt.Errorf("ci/cd run webhook scope_id is not configured")
	}
	return selected, nil
}

func validatePlanRequest(request PlanRequest) error {
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
	if err := contract.ValidateSafePlanKey("ci/cd run planner", request.PlanKey); err != nil {
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

// ConfiguredGitHubScopes returns every validated target scope that authorizes
// one exact GitHub repository. A verified webhook never selects GitLab targets.
func ConfiguredGitHubScopes(configuration, repository string) ([]string, error) {
	targets, err := parseCICDRunRuntimeTargets(configuration)
	if err != nil {
		return nil, err
	}
	repository = shard.NormalizeRepository(repository)
	if repository == "" {
		return nil, nil
	}
	scopes := make([]string, 0)
	for _, target := range targets {
		if target.Provider == "github_actions" && shard.NormalizeRepository(target.Repository) == repository {
			scopes = append(scopes, target.ScopeID)
		}
	}
	sort.Strings(scopes)
	return scopes, nil
}

// ConfiguredGitHubTargetRepositories maps every validated GitHub Actions
// target scope to its configured repository for pending-delivery admission.
func ConfiguredGitHubTargetRepositories(configuration string) (map[string]string, error) {
	targets, err := parseCICDRunRuntimeTargets(configuration)
	if err != nil {
		return nil, err
	}
	if err := validateUniqueCICDRunTargets(targets); err != nil {
		return nil, err
	}
	repositories := make(map[string]string)
	for _, target := range targets {
		if target.Provider == "github_actions" {
			repositories[target.ScopeID] = shard.NormalizeRepository(target.Repository)
		}
	}
	return repositories, nil
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

func cicdRunRunID(instance workflow.CollectorInstance, planKey string, triggerKind workflow.TriggerKind) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorCICDRun,
		strings.TrimSpace(instance.InstanceID),
		triggerKind,
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
