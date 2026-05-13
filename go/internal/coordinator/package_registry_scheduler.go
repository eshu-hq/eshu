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

// PackageRegistryPlanRequest carries one package-registry collector planning
// request.
type PackageRegistryPlanRequest struct {
	Instance   workflow.CollectorInstance
	ObservedAt time.Time
	PlanKey    string
}

// PackageRegistryWorkPlanner plans workflow rows for configured
// package-registry targets without opening registry connections.
type PackageRegistryWorkPlanner struct{}

type packageRegistryRuntimeConfiguration struct {
	Targets []packageRegistryTargetConfiguration `json:"targets"`
}

type packageRegistryTargetConfiguration struct {
	Provider     string   `json:"provider"`
	Ecosystem    string   `json:"ecosystem"`
	Registry     string   `json:"registry"`
	ScopeID      string   `json:"scope_id"`
	Namespace    string   `json:"namespace"`
	Packages     []string `json:"packages"`
	PackageLimit int      `json:"package_limit"`
	VersionLimit int      `json:"version_limit"`
	Visibility   string   `json:"visibility"`
	SourceURI    string   `json:"source_uri"`
	MetadataURL  string   `json:"metadata_url"`
}

// PlanPackageRegistryWork returns one run and one work item per configured
// package-registry target.
func (p PackageRegistryWorkPlanner) PlanPackageRegistryWork(
	_ context.Context,
	request PackageRegistryPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validatePackageRegistryPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	targets, err := parsePackageRegistryRuntimeTargets(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	if err := validateUniquePackageRegistryTargets(targets); err != nil {
		return workflow.Run{}, nil, err
	}

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              packageRegistryRunID(request.Instance, request.PlanKey),
		TriggerKind:        packageRegistryTriggerKind(request.Instance),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  packageRegistryRequestedScopeSet(request.Instance, targets),
		RequestedCollector: string(scope.CollectorPackageRegistry),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for _, target := range targets {
		item, err := packageRegistryWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validatePackageRegistryPlanRequest(request PackageRegistryPlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("package registry plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorPackageRegistry {
		return fmt.Errorf("package registry planner requires collector_kind %q", scope.CollectorPackageRegistry)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("package registry planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("package registry planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("package registry planner observed_at must not be zero")
	}
	if err := validateSafePlanKey("package registry planner", request.PlanKey); err != nil {
		return err
	}
	return nil
}

func parsePackageRegistryRuntimeTargets(raw string) ([]packageRegistryTargetConfiguration, error) {
	if err := workflow.ValidatePackageRegistryCollectorConfiguration(raw); err != nil {
		return nil, err
	}
	var decoded packageRegistryRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("decode package registry collector configuration: %w", err)
	}
	targets := make([]packageRegistryTargetConfiguration, 0, len(decoded.Targets))
	for _, target := range decoded.Targets {
		target.Provider = strings.TrimSpace(target.Provider)
		target.Ecosystem = strings.TrimSpace(target.Ecosystem)
		target.Registry = strings.TrimRight(strings.TrimSpace(target.Registry), "/")
		target.ScopeID = strings.TrimSpace(target.ScopeID)
		target.Namespace = strings.TrimSpace(target.Namespace)
		target.SourceURI = strings.TrimSpace(target.SourceURI)
		target.MetadataURL = strings.TrimRight(strings.TrimSpace(target.MetadataURL), "/")
		targets = append(targets, target)
	}
	return targets, nil
}

func validateUniquePackageRegistryTargets(targets []packageRegistryTargetConfiguration) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		scopeID := strings.TrimSpace(target.ScopeID)
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate package registry target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

func packageRegistryRunID(instance workflow.CollectorInstance, planKey string) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorPackageRegistry,
		strings.TrimSpace(instance.InstanceID),
		packageRegistryTriggerKind(instance),
		strings.TrimSpace(planKey),
	)
}

func packageRegistryTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

func packageRegistryRequestedScopeSet(
	instance workflow.CollectorInstance,
	targets []packageRegistryTargetConfiguration,
) string {
	type requestedTarget struct {
		ScopeID   string `json:"scope_id"`
		Ecosystem string `json:"ecosystem"`
		Provider  string `json:"provider"`
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
			Ecosystem: strings.TrimSpace(target.Ecosystem),
			Provider:  strings.TrimSpace(target.Provider),
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

func packageRegistryWorkItem(
	instance workflow.CollectorInstance,
	target packageRegistryTargetConfiguration,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	scopeID := strings.TrimSpace(target.ScopeID)
	generationID := "package_registry:" + facts.StableID("PackageRegistryWorkflowGeneration", map[string]any{
		"instance_id": strings.TrimSpace(instance.InstanceID),
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    scopeID,
	})
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorPackageRegistry, instance.InstanceID, generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorPackageRegistry,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        string(scope.CollectorPackageRegistry),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorPackageRegistry, strings.TrimSpace(instance.InstanceID), strings.TrimSpace(target.Ecosystem)),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}
