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

// PackageRegistryPlanRequest carries one package-registry collector planning
// request.
type PackageRegistryPlanRequest struct {
	Instance            workflow.CollectorInstance
	ObservedAt          time.Time
	PlanKey             string
	OwnedPackageTargets []workflow.OwnedPackageDependencyTarget
}

// PackageRegistryWorkPlanner plans workflow rows for configured
// package-registry targets without opening registry connections.
type PackageRegistryWorkPlanner struct{}

type packageRegistryRuntimeConfiguration struct {
	Targets                 []packageRegistryTargetConfiguration   `json:"targets"`
	DeriveFromOwnedPackages packageRegistryDerivationConfiguration `json:"derive_from_owned_packages"`
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
	Derived      bool     `json:"-"`
	PackageName  string   `json:"-"`
	TargetClass  string   `json:"-"`
}

type packageRegistryDerivationConfiguration struct {
	Enabled      bool     `json:"enabled"`
	Ecosystems   []string `json:"ecosystems"`
	PlanningMode string   `json:"planning_mode"`
	TargetLimit  int      `json:"target_limit"`
	PackageLimit int      `json:"package_limit"`
	VersionLimit int      `json:"version_limit"`
}

type packageRegistryDerivationResult struct {
	Targets        []packageRegistryTargetConfiguration
	SkippedTargets []derivedTargetSkipEvidence
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
	derivation := decodedPackageRegistryDerivation(targets, request)
	targets = appendPackageRegistryDerivedTargets(targets, derivation.Targets)

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              packageRegistryRunID(request.Instance, request.PlanKey),
		TriggerKind:        packageRegistryTriggerKind(request.Instance),
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  packageRegistryRequestedScopeSet(request.Instance, targets, derivation.SkippedTargets),
		RequestedCollector: string(scope.CollectorPackageRegistry),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(targets))
	for ordinal, target := range targets {
		item, err := packageRegistryWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt, ordinal)
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
		target.TargetClass = packageRegistryConfiguredTargetClass(target)
		targets = append(targets, target)
	}
	return targets, nil
}

func decodedPackageRegistryDerivation(
	configured []packageRegistryTargetConfiguration,
	request PackageRegistryPlanRequest,
) packageRegistryDerivationResult {
	var decoded packageRegistryRuntimeConfiguration
	if err := json.Unmarshal([]byte(request.Instance.Configuration), &decoded); err != nil {
		return packageRegistryDerivationResult{}
	}
	return derivePackageRegistryTargets(configured, decoded.DeriveFromOwnedPackages, request.OwnedPackageTargets)
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
	skippedTargets []derivedTargetSkipEvidence,
) string {
	type requestedTarget struct {
		ScopeID   string `json:"scope_id"`
		Ecosystem string `json:"ecosystem"`
		Provider  string `json:"provider"`
		Package   string `json:"package_name,omitempty"`
		Derived   bool   `json:"derived,omitempty"`
		Class     string `json:"target_class,omitempty"`
	}
	payload := struct {
		CollectorInstanceID string                      `json:"collector_instance_id"`
		Targets             []requestedTarget           `json:"targets"`
		SkippedTargets      []derivedTargetSkipEvidence `json:"skipped_targets,omitempty"`
	}{
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		Targets:             make([]requestedTarget, 0, len(targets)),
		SkippedTargets:      skippedTargets,
	}
	for _, target := range targets {
		payload.Targets = append(payload.Targets, requestedTarget{
			ScopeID:   strings.TrimSpace(target.ScopeID),
			Ecosystem: strings.TrimSpace(target.Ecosystem),
			Provider:  strings.TrimSpace(target.Provider),
			Package:   strings.TrimSpace(target.PackageName),
			Derived:   target.Derived,
			Class:     packageRegistryTargetClass(target),
		})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func appendPackageRegistryDerivedTargets(
	configured []packageRegistryTargetConfiguration,
	derived []packageRegistryTargetConfiguration,
) []packageRegistryTargetConfiguration {
	if len(derived) == 0 {
		return configured
	}
	seen := make(map[string]struct{}, len(configured)+len(derived))
	for _, target := range configured {
		seen[strings.TrimSpace(target.ScopeID)] = struct{}{}
	}
	for _, target := range derived {
		if _, ok := seen[strings.TrimSpace(target.ScopeID)]; ok {
			continue
		}
		seen[strings.TrimSpace(target.ScopeID)] = struct{}{}
		configured = append(configured, target)
	}
	sort.SliceStable(configured, func(i, j int) bool {
		return targetClassRank(packageRegistryTargetClass(configured[i])) <
			targetClassRank(packageRegistryTargetClass(configured[j]))
	})
	return configured
}

func derivePackageRegistryTargets(
	configured []packageRegistryTargetConfiguration,
	derivation packageRegistryDerivationConfiguration,
	owned []workflow.OwnedPackageDependencyTarget,
) packageRegistryDerivationResult {
	if !derivation.Enabled {
		return packageRegistryDerivationResult{}
	}
	limit := packageRegistryDerivedTargetLimit(derivation.TargetLimit)
	packageLimit := derivation.PackageLimit
	if packageLimit <= 0 {
		packageLimit = 1
	}
	versionLimit := derivation.VersionLimit
	if versionLimit <= 0 {
		versionLimit = 200
	}
	ecosystems := packageRegistryDerivationEcosystems(derivation.Ecosystems)
	seen := make(map[string]struct{}, len(configured)+len(owned))
	for _, target := range configured {
		seen[strings.TrimSpace(target.ScopeID)] = struct{}{}
	}
	out := make([]packageRegistryTargetConfiguration, 0, minInt(limit, len(owned)))
	skipped := 0
	for _, target := range owned {
		if !stringSetContains(ecosystems, target.Ecosystem) {
			continue
		}
		derived, ok := packageRegistryTargetForOwnedPackage(target, packageLimit, versionLimit)
		if !ok {
			continue
		}
		if _, exists := seen[derived.ScopeID]; exists {
			continue
		}
		seen[derived.ScopeID] = struct{}{}
		if len(out) >= limit {
			skipped++
			continue
		}
		out = append(out, derived)
	}
	return packageRegistryDerivationResult{
		Targets: out,
		SkippedTargets: derivedTargetBudgetSkipEvidence(
			string(scope.CollectorPackageRegistry),
			limit,
			len(out),
			skipped,
			sortedStringSetValues(ecosystems),
			nil,
		),
	}
}

const (
	defaultDerivedPackageTargets = 100
	maxDerivedPackageTargets     = 5000
)

func packageRegistryDerivedTargetLimit(raw int) int {
	limit := derivationLimit(raw, defaultDerivedPackageTargets)
	if limit > maxDerivedPackageTargets {
		return maxDerivedPackageTargets
	}
	return limit
}

func packageRegistryWorkItem(
	instance workflow.CollectorInstance,
	target packageRegistryTargetConfiguration,
	runID string,
	planKey string,
	observedAt time.Time,
	ordinal int,
) (workflow.WorkItem, error) {
	scopeID := strings.TrimSpace(target.ScopeID)
	createdAt := targetCreatedAt(observedAt, ordinal)
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
		FairnessKey:         fmt.Sprintf("%s:%s:%s:%s", scope.CollectorPackageRegistry, strings.TrimSpace(instance.InstanceID), packageRegistryTargetClass(target), strings.TrimSpace(target.Ecosystem)),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           createdAt,
		UpdatedAt:           createdAt,
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}
