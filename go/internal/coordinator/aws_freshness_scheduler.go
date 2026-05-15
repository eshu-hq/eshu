package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// AWSFreshnessPlanRequest carries claimed AWS freshness triggers into workflow
// work planning.
type AWSFreshnessPlanRequest struct {
	Instance   workflow.CollectorInstance
	Triggers   []freshness.StoredTrigger
	ObservedAt time.Time
	PlanKey    string
}

// AWSFreshnessWorkPlanner plans targeted AWS collector work from coalesced
// freshness triggers.
type AWSFreshnessWorkPlanner struct{}

type awsFreshnessRuntimeConfiguration struct {
	TargetScopes []awsFreshnessTargetScopeConfiguration `json:"target_scopes"`
}

type awsFreshnessTargetScopeConfiguration struct {
	AccountID       string   `json:"account_id"`
	AllowedRegions  []string `json:"allowed_regions"`
	AllowedServices []string `json:"allowed_services"`
}

// PlanAWSFreshnessWork returns one workflow run and one item per unique AWS
// target tuple represented by the supplied triggers.
func (p AWSFreshnessWorkPlanner) PlanAWSFreshnessWork(
	_ context.Context,
	request AWSFreshnessPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validateAWSFreshnessPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	scopes, err := parseAWSFreshnessTargetScopes(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	targets, err := authorizedAWSFreshnessTargets(request.Triggers, scopes)
	if err != nil {
		return workflow.Run{}, nil, err
	}

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              awsFreshnessRunID(request.Instance, request.PlanKey),
		TriggerKind:        workflow.TriggerKindWebhook,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  awsFreshnessRequestedScopeSet(request.Instance, targets),
		RequestedCollector: string(scope.CollectorAWS),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for _, target := range targets {
		item, err := awsFreshnessWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validateAWSFreshnessPlanRequest(request AWSFreshnessPlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("AWS freshness plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorAWS {
		return fmt.Errorf("AWS freshness planner requires collector_kind %q", scope.CollectorAWS)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("AWS freshness planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("AWS freshness planner requires claim-enabled collector instance")
	}
	if len(request.Triggers) == 0 {
		return fmt.Errorf("AWS freshness planner requires at least one trigger")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("AWS freshness planner observed_at must not be zero")
	}
	if err := validateSafePlanKey("AWS freshness planner", request.PlanKey); err != nil {
		return err
	}
	return nil
}

func parseAWSFreshnessTargetScopes(raw string) ([]awsFreshnessTargetScopeConfiguration, error) {
	var decoded awsFreshnessRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("decode AWS collector configuration: %w", err)
	}
	if len(decoded.TargetScopes) == 0 {
		return nil, fmt.Errorf("AWS collector configuration requires target_scopes")
	}
	scopes := make([]awsFreshnessTargetScopeConfiguration, 0, len(decoded.TargetScopes))
	for index, target := range decoded.TargetScopes {
		mapped, err := normalizeAWSFreshnessTargetScope(target)
		if err != nil {
			return nil, fmt.Errorf("target_scopes[%d]: %w", index, err)
		}
		scopes = append(scopes, mapped)
	}
	return scopes, nil
}

func normalizeAWSFreshnessTargetScope(
	target awsFreshnessTargetScopeConfiguration,
) (awsFreshnessTargetScopeConfiguration, error) {
	target.AccountID = strings.TrimSpace(target.AccountID)
	if !isAWSFreshnessAccountID(target.AccountID) {
		return awsFreshnessTargetScopeConfiguration{}, fmt.Errorf("account_id must be a 12 digit AWS account ID")
	}
	regions, err := normalizeAWSFreshnessList(target.AllowedRegions, "allowed_regions", nil)
	if err != nil {
		return awsFreshnessTargetScopeConfiguration{}, err
	}
	services, err := normalizeAWSFreshnessList(target.AllowedServices, "allowed_services", awsruntime.SupportsServiceKind)
	if err != nil {
		return awsFreshnessTargetScopeConfiguration{}, err
	}
	target.AllowedRegions = regions
	target.AllowedServices = services
	return target, nil
}

func authorizedAWSFreshnessTargets(
	triggers []freshness.StoredTrigger,
	scopes []awsFreshnessTargetScopeConfiguration,
) ([]freshness.Target, error) {
	targetsByKey := make(map[string]freshness.Target, len(triggers))
	for _, trigger := range triggers {
		if err := trigger.Validate(); err != nil {
			return nil, err
		}
		target := trigger.Target()
		if !awsFreshnessTargetAuthorized(target, scopes) {
			return nil, fmt.Errorf("AWS freshness target %q is not authorized for collector instance", target.FreshnessKey())
		}
		targetsByKey[target.FreshnessKey()] = target
	}
	keys := make([]string, 0, len(targetsByKey))
	for key := range targetsByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	targets := make([]freshness.Target, 0, len(keys))
	for _, key := range keys {
		targets = append(targets, targetsByKey[key])
	}
	return targets, nil
}

func awsFreshnessTargetAuthorized(
	target freshness.Target,
	scopes []awsFreshnessTargetScopeConfiguration,
) bool {
	for _, candidate := range scopes {
		if candidate.AccountID != target.AccountID {
			continue
		}
		if !slices.Contains(candidate.AllowedRegions, target.Region) {
			continue
		}
		if !slices.Contains(candidate.AllowedServices, target.ServiceKind) {
			continue
		}
		return true
	}
	return false
}

func awsFreshnessRunID(instance workflow.CollectorInstance, planKey string) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorAWS,
		strings.TrimSpace(instance.InstanceID),
		workflow.TriggerKindWebhook,
		strings.TrimSpace(planKey),
	)
}

func awsFreshnessRequestedScopeSet(
	instance workflow.CollectorInstance,
	targets []freshness.Target,
) string {
	type requestedTarget struct {
		AccountID   string `json:"account_id"`
		Region      string `json:"region"`
		ServiceKind string `json:"service_kind"`
		ScopeID     string `json:"scope_id"`
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
			AccountID:   target.AccountID,
			Region:      target.Region,
			ServiceKind: target.ServiceKind,
			ScopeID:     target.ScopeID(),
		})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func awsFreshnessWorkItem(
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
	generationID := "aws_freshness:" + facts.StableID("AWSFreshnessWorkflowGeneration", map[string]any{
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

func normalizeAWSFreshnessList(values []string, field string, accept func(string) bool) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("%s is required", field)
	}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		switch {
		case value == "":
			return nil, fmt.Errorf("%s must not contain empty entries", field)
		case value == "*":
			return nil, fmt.Errorf("%s must not contain wildcard entries", field)
		case accept != nil && !accept(value):
			return nil, fmt.Errorf("unsupported AWS service_kind %q", value)
		default:
			cleaned = append(cleaned, value)
		}
	}
	return cleaned, nil
}

func isAWSFreshnessAccountID(value string) bool {
	if len(value) != 12 {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
