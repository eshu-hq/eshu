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

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	awsScheduledGlobalRegion                   = "aws-global"
	awsScheduledSkipReasonRegionalAWSGlobal    = "regional_service_aws_global"
	awsScheduledSkipReasonGlobalRegionalRegion = "global_service_regional_region"
)

// AWSScheduledPlanRequest carries one scheduled AWS collector planning request.
type AWSScheduledPlanRequest struct {
	Instance   workflow.CollectorInstance
	ObservedAt time.Time
	PlanKey    string
}

// AWSScheduledWorkPlanner plans bounded AWS work from configured target scopes.
type AWSScheduledWorkPlanner struct{}

// PlanAWSScheduledWork returns one scheduled run and one work item per valid
// configured AWS account, region, and service tuple. When every configured
// tuple is skipped as invalid, it returns a completed audit-only run with the
// skipped targets recorded in requested_scope_set and no work items.
func (p AWSScheduledWorkPlanner) PlanAWSScheduledWork(
	_ context.Context,
	request AWSScheduledPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validateAWSScheduledPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	scopes, err := parseAWSFreshnessTargetScopes(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	targetPlan := planAWSScheduledTargets(scopes)

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              awsScheduledRunID(request.Instance, request.PlanKey),
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  awsScheduledRequestedScopeSet(request.Instance, targetPlan),
		RequestedCollector: string(scope.CollectorAWS),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	targets := targetPlan.Targets
	if len(targets) == 0 && len(targetPlan.SkippedTargets) > 0 {
		run.Status = workflow.RunStatusComplete
		run.FinishedAt = observedAt
		return run, nil, nil
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for _, target := range targets {
		item, err := awsScheduledWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validateAWSScheduledPlanRequest(request AWSScheduledPlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("AWS scheduled plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorAWS {
		return fmt.Errorf("AWS scheduled planner requires collector_kind %q", scope.CollectorAWS)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("AWS scheduled planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("AWS scheduled planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("AWS scheduled planner observed_at must not be zero")
	}
	if err := validateSafePlanKey("AWS scheduled planner", request.PlanKey); err != nil {
		return err
	}
	return nil
}

type awsScheduledTargetPlan struct {
	Targets        []freshness.Target
	SkippedTargets []awsScheduledSkippedTarget
}

type awsScheduledSkippedTarget struct {
	AccountID   string
	Region      string
	ServiceKind string
	Reason      string
}

func awsScheduledScanEnabled(raw string) (bool, error) {
	var decoded awsFreshnessRuntimeConfiguration
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		normalized = "{}"
	}
	if err := json.Unmarshal([]byte(normalized), &decoded); err != nil {
		return false, fmt.Errorf("decode AWS collector configuration: %w", err)
	}
	return decoded.ScheduledScanEnabled, nil
}

func planAWSScheduledTargets(scopes []awsFreshnessTargetScopeConfiguration) awsScheduledTargetPlan {
	byKey := map[string]freshness.Target{}
	skippedByKey := map[string]awsScheduledSkippedTarget{}
	for _, targetScope := range scopes {
		for _, region := range targetScope.AllowedRegions {
			for _, serviceKind := range targetScope.AllowedServices {
				target := freshness.Target{
					AccountID:   strings.TrimSpace(targetScope.AccountID),
					Region:      strings.TrimSpace(region),
					ServiceKind: strings.TrimSpace(serviceKind),
				}
				if reason, ok := awsScheduledTargetAllowed(target); !ok {
					skippedByKey[target.FreshnessKey()+"|"+reason] = awsScheduledSkippedTarget{
						AccountID:   target.AccountID,
						Region:      target.Region,
						ServiceKind: target.ServiceKind,
						Reason:      reason,
					}
					continue
				}
				byKey[target.FreshnessKey()] = target
			}
		}
	}
	return awsScheduledTargetPlan{
		Targets:        sortedAWSScheduledTargets(byKey),
		SkippedTargets: sortedAWSScheduledSkippedTargets(skippedByKey),
	}
}

func awsScheduledTargetAllowed(target freshness.Target) (string, bool) {
	if target.Region == awsScheduledGlobalRegion {
		if awsScheduledServiceGlobalOnly(target.ServiceKind) {
			return "", true
		}
		return awsScheduledSkipReasonRegionalAWSGlobal, false
	}
	if awsScheduledServiceGlobalOnly(target.ServiceKind) {
		return awsScheduledSkipReasonGlobalRegionalRegion, false
	}
	return "", true
}

func awsScheduledServiceGlobalOnly(serviceKind string) bool {
	switch serviceKind {
	case awscloud.ServiceCloudFront, awscloud.ServiceIAM, awscloud.ServiceRoute53:
		return true
	default:
		return false
	}
}

func sortedAWSScheduledTargets(byKey map[string]freshness.Target) []freshness.Target {
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	targets := make([]freshness.Target, 0, len(keys))
	for _, key := range keys {
		targets = append(targets, byKey[key])
	}
	return targets
}

func sortedAWSScheduledSkippedTargets(byKey map[string]awsScheduledSkippedTarget) []awsScheduledSkippedTarget {
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	skippedTargets := make([]awsScheduledSkippedTarget, 0, len(keys))
	for _, key := range keys {
		skippedTargets = append(skippedTargets, byKey[key])
	}
	return skippedTargets
}

func awsScheduledRequestedScopeSet(
	instance workflow.CollectorInstance,
	targetPlan awsScheduledTargetPlan,
) string {
	type requestedTarget struct {
		AccountID   string `json:"account_id"`
		Region      string `json:"region"`
		ServiceKind string `json:"service_kind"`
		ScopeID     string `json:"scope_id"`
	}
	type skippedTarget struct {
		AccountID   string `json:"account_id"`
		Region      string `json:"region"`
		ServiceKind string `json:"service_kind"`
		Reason      string `json:"reason"`
	}
	payload := struct {
		CollectorInstanceID string            `json:"collector_instance_id"`
		Targets             []requestedTarget `json:"targets"`
		SkippedTargets      []skippedTarget   `json:"skipped_targets"`
	}{
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		Targets:             make([]requestedTarget, 0, len(targetPlan.Targets)),
		SkippedTargets:      make([]skippedTarget, 0, len(targetPlan.SkippedTargets)),
	}
	for _, target := range targetPlan.Targets {
		payload.Targets = append(payload.Targets, requestedTarget{
			AccountID:   target.AccountID,
			Region:      target.Region,
			ServiceKind: target.ServiceKind,
			ScopeID:     target.ScopeID(),
		})
	}
	for _, target := range targetPlan.SkippedTargets {
		payload.SkippedTargets = append(payload.SkippedTargets, skippedTarget(target))
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func awsScheduledRunID(instance workflow.CollectorInstance, planKey string) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorAWS,
		strings.TrimSpace(instance.InstanceID),
		workflow.TriggerKindSchedule,
		strings.TrimSpace(planKey),
	)
}

func awsScheduledWorkItem(
	instance workflow.CollectorInstance,
	target freshness.Target,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	acceptanceUnitID, err := target.AcceptanceUnitID()
	if err != nil {
		return workflow.WorkItem{}, err
	}
	scopeID := target.ScopeID()
	generationID := "aws_schedule:" + facts.StableID("AWSScheduledWorkflowGeneration", map[string]any{
		"instance_id": strings.TrimSpace(instance.InstanceID),
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    scopeID,
	})
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorAWS, strings.TrimSpace(instance.InstanceID), generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorAWS,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        string(scope.CollectorAWS),
		ScopeID:             scopeID,
		AcceptanceUnitID:    acceptanceUnitID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorAWS, strings.TrimSpace(instance.InstanceID), target.AccountID),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}
