package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/acr"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/dockerhub"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ecr"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/gar"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/ghcr"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/harbor"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/jfrog"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// OCIRegistryPlanRequest carries one OCI registry collector planning request.
type OCIRegistryPlanRequest struct {
	Instance   workflow.CollectorInstance
	ObservedAt time.Time
	PlanKey    string
}

// OCIRegistryWorkPlanner plans workflow rows for configured OCI registry
// repository targets without opening any registry connection.
type OCIRegistryWorkPlanner struct{}

type ociRegistryRuntimeConfiguration struct {
	Targets []ociRegistryTargetConfiguration `json:"targets"`
}

type ociRegistryTargetConfiguration struct {
	Provider      string   `json:"provider"`
	Registry      string   `json:"registry"`
	BaseURL       string   `json:"base_url"`
	RepositoryKey string   `json:"repository_key"`
	Repository    string   `json:"repository"`
	Region        string   `json:"region"`
	RegistryID    string   `json:"registry_id"`
	RegistryHost  string   `json:"registry_host"`
	References    []string `json:"references"`
	TagLimit      int      `json:"tag_limit"`
}

// PlanOCIRegistryWork returns one run and one work item per configured OCI
// registry repository target.
func (p OCIRegistryWorkPlanner) PlanOCIRegistryWork(
	_ context.Context,
	request OCIRegistryPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validateOCIRegistryPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	targets, err := parseOCIRegistryRuntimeTargets(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	if len(targets) == 0 {
		return workflow.Run{}, nil, nil
	}
	if err := validateUniqueOCIRegistryTargets(targets); err != nil {
		return workflow.Run{}, nil, err
	}

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              ociRegistryRunID(request.Instance, request.PlanKey),
		TriggerKind:        ociRegistryTriggerKind(request.Instance),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  ociRegistryRequestedScopeSet(request.Instance, targets),
		RequestedCollector: string(scope.CollectorOCIRegistry),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for _, target := range targets {
		item, err := ociRegistryWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validateOCIRegistryPlanRequest(request OCIRegistryPlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("OCI registry plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorOCIRegistry {
		return fmt.Errorf("OCI registry planner requires collector_kind %q", scope.CollectorOCIRegistry)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("OCI registry planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("OCI registry planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("OCI registry planner observed_at must not be zero")
	}
	if err := validateSafePlanKey("OCI registry planner", request.PlanKey); err != nil {
		return err
	}
	return nil
}

func validateSafePlanKey(owner string, planKey string) error {
	planKey = strings.TrimSpace(planKey)
	if planKey == "" {
		return fmt.Errorf("%s plan_key must not be blank", owner)
	}
	if strings.ContainsAny(planKey, `/\`) {
		return fmt.Errorf("%s plan_key must not include raw source locator material", owner)
	}
	for _, char := range planKey {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		switch char {
		case '.', '_', '-':
			continue
		default:
			return fmt.Errorf("%s plan_key contains unsupported character %q", owner, char)
		}
	}
	return nil
}

func parseOCIRegistryRuntimeTargets(raw string) ([]ociRegistryTargetConfiguration, error) {
	if err := workflow.ValidateOCIRegistryCollectorConfiguration(raw); err != nil {
		return nil, err
	}
	var decoded ociRegistryRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("decode OCI registry collector configuration: %w", err)
	}
	targets := make([]ociRegistryTargetConfiguration, 0, len(decoded.Targets))
	for _, target := range decoded.Targets {
		target.Provider = strings.TrimSpace(target.Provider)
		target.Registry = strings.Trim(strings.TrimSpace(target.Registry), "/")
		target.BaseURL = strings.TrimRight(strings.TrimSpace(target.BaseURL), "/")
		target.RepositoryKey = strings.Trim(strings.TrimSpace(target.RepositoryKey), "/")
		target.Repository = strings.Trim(strings.TrimSpace(target.Repository), "/")
		target.Region = strings.TrimSpace(target.Region)
		target.RegistryID = strings.TrimSpace(target.RegistryID)
		target.RegistryHost = strings.TrimRight(strings.TrimSpace(target.RegistryHost), "/")
		targets = append(targets, target)
	}
	return targets, nil
}

func validateUniqueOCIRegistryTargets(targets []ociRegistryTargetConfiguration) error {
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		identity, err := ociRegistryTargetIdentity(target)
		if err != nil {
			return err
		}
		if _, ok := seen[identity.ScopeID]; ok {
			return fmt.Errorf("duplicate OCI registry target scope_id %q", identity.ScopeID)
		}
		seen[identity.ScopeID] = struct{}{}
	}
	return nil
}

func ociRegistryRunID(instance workflow.CollectorInstance, planKey string) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorOCIRegistry,
		strings.TrimSpace(instance.InstanceID),
		ociRegistryTriggerKind(instance),
		strings.TrimSpace(planKey),
	)
}

func ociRegistryTriggerKind(instance workflow.CollectorInstance) workflow.TriggerKind {
	if instance.Bootstrap {
		return workflow.TriggerKindBootstrap
	}
	return workflow.TriggerKindSchedule
}

func ociRegistryRequestedScopeSet(
	instance workflow.CollectorInstance,
	targets []ociRegistryTargetConfiguration,
) string {
	type requestedTarget struct {
		ScopeID    string `json:"scope_id"`
		Provider   string `json:"provider"`
		Repository string `json:"repository"`
	}
	payload := struct {
		CollectorInstanceID string            `json:"collector_instance_id"`
		Targets             []requestedTarget `json:"targets"`
	}{
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		Targets:             make([]requestedTarget, 0, len(targets)),
	}
	for _, target := range targets {
		identity, err := ociRegistryTargetIdentity(target)
		if err != nil {
			continue
		}
		payload.Targets = append(payload.Targets, requestedTarget{
			ScopeID:    identity.ScopeID,
			Provider:   string(identity.Provider),
			Repository: identity.Repository,
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

func ociRegistryWorkItem(
	instance workflow.CollectorInstance,
	target ociRegistryTargetConfiguration,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	identity, err := ociRegistryTargetIdentity(target)
	if err != nil {
		return workflow.WorkItem{}, err
	}
	generationID := "oci_registry:" + facts.StableID("OCIRegistryWorkflowGeneration", map[string]any{
		"instance_id": strings.TrimSpace(instance.InstanceID),
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    identity.ScopeID,
	})
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorOCIRegistry, instance.InstanceID, generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorOCIRegistry,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        string(scope.CollectorOCIRegistry),
		ScopeID:             identity.ScopeID,
		AcceptanceUnitID:    identity.ScopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorOCIRegistry, strings.TrimSpace(instance.InstanceID), string(identity.Provider)),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}

func ociRegistryTargetIdentity(target ociRegistryTargetConfiguration) (ociregistry.NormalizedRepositoryIdentity, error) {
	var identity ociregistry.RepositoryIdentity
	var err error
	provider := ociregistry.Provider(strings.TrimSpace(target.Provider))
	switch provider {
	case ociregistry.ProviderDockerHub:
		repository, repositoryErr := dockerhub.RepositoryName(target.Repository)
		if repositoryErr != nil {
			return ociregistry.NormalizedRepositoryIdentity{}, repositoryErr
		}
		registry := strings.Trim(strings.TrimSpace(target.Registry), "/")
		if registry == "" {
			registry = dockerhub.RegistryHost
		}
		identity = ociregistry.RepositoryIdentity{
			Provider:   ociregistry.ProviderDockerHub,
			Registry:   registry,
			Repository: repository,
		}
	case ociregistry.ProviderGHCR:
		repository, repositoryErr := ghcr.RepositoryName(target.Repository)
		if repositoryErr != nil {
			return ociregistry.NormalizedRepositoryIdentity{}, repositoryErr
		}
		registry := strings.Trim(strings.TrimSpace(target.Registry), "/")
		if registry == "" {
			registry = ghcr.RegistryHost
		}
		identity = ociregistry.RepositoryIdentity{
			Provider:   ociregistry.ProviderGHCR,
			Registry:   registry,
			Repository: repository,
		}
	case ociregistry.ProviderJFrog:
		identity, err = jfrog.RepositoryIdentity(target.BaseURL, target.RepositoryKey, target.Repository)
	case ociregistry.ProviderECR:
		registry := firstNonBlank(target.RegistryHost, target.Registry)
		if registry == "" {
			registry, err = ecr.PrivateRegistryHost(target.RegistryID, target.Region)
			if err != nil {
				return ociregistry.NormalizedRepositoryIdentity{}, err
			}
		}
		identity = ecr.RepositoryIdentity(registry, target.Repository)
	case ociregistry.ProviderHarbor:
		identity, err = harbor.RepositoryIdentity(target.BaseURL, target.Repository)
	case ociregistry.ProviderGoogleArtifactRegistry:
		identity, err = gar.RepositoryIdentity(firstNonBlank(target.RegistryHost, target.Registry), target.Repository)
	case ociregistry.ProviderAzureContainerRegistry:
		identity, err = acr.RepositoryIdentity(firstNonBlank(target.RegistryHost, target.Registry), target.Repository)
	default:
		return ociregistry.NormalizedRepositoryIdentity{}, fmt.Errorf("unsupported OCI registry provider %q", target.Provider)
	}
	if err != nil {
		return ociregistry.NormalizedRepositoryIdentity{}, err
	}
	return ociregistry.NormalizeRepositoryIdentity(identity)
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
