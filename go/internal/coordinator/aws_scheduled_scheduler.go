package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// AWSScheduledPlanRequest carries one scheduled AWS collector planning request.
type AWSScheduledPlanRequest struct {
	Instance   workflow.CollectorInstance
	ObservedAt time.Time
	PlanKey    string
}

// AWSScheduledWorkPlanner plans bounded AWS work from configured target scopes.
type AWSScheduledWorkPlanner struct{}

// PlanAWSScheduledWork returns one scheduled run and one work item per
// configured AWS account, region, and service tuple.
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
	targets := awsScheduledTargets(scopes)

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              awsScheduledRunID(request.Instance, request.PlanKey),
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  awsFreshnessRequestedScopeSet(request.Instance, targets),
		RequestedCollector: string(scope.CollectorAWS),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
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

func awsScheduledScanEnabled(raw string) (bool, error) {
	var decoded awsFreshnessRuntimeConfiguration
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &decoded); err != nil {
		return false, fmt.Errorf("decode AWS collector configuration: %w", err)
	}
	return decoded.ScheduledScanEnabled, nil
}

func awsScheduledTargets(scopes []awsFreshnessTargetScopeConfiguration) []freshness.Target {
	byKey := map[string]freshness.Target{}
	for _, targetScope := range scopes {
		for _, region := range targetScope.AllowedRegions {
			for _, serviceKind := range targetScope.AllowedServices {
				target := freshness.Target{
					AccountID:   strings.TrimSpace(targetScope.AccountID),
					Region:      strings.TrimSpace(region),
					ServiceKind: strings.TrimSpace(serviceKind),
				}
				byKey[target.FreshnessKey()] = target
			}
		}
	}
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
