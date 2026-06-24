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

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/gcpruntime"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// GCPPlanRequest carries one live GCP Cloud Asset Inventory planning request.
type GCPPlanRequest struct {
	// Instance is the durable GCP collector instance to plan work for.
	Instance workflow.CollectorInstance
	// ObservedAt anchors the run and work-item timestamps for this reconcile.
	ObservedAt time.Time
	// PlanKey makes repeated planning for the same instance and interval
	// idempotent.
	PlanKey string
	// ScopeIDs optionally restricts planning to a subset of configured scopes.
	ScopeIDs []string
}

// GCPWorkPlanner plans claim-driven GCP collector workflow rows without
// resolving credentials or contacting Google Cloud.
type GCPWorkPlanner struct{}

type gcpRuntimeConfiguration struct {
	LiveCollectionEnabled bool                    `json:"live_collection_enabled"`
	Scopes                []gcpScopeConfiguration `json:"scopes"`
}

type gcpScopeConfiguration struct {
	ScopeID         string `json:"scope_id"`
	ParentScopeKind string `json:"parent_scope_kind"`
	ParentScopeID   string `json:"parent_scope_id"`
	AssetTypeFamily string `json:"asset_type_family"`
	ContentFamily   string `json:"content_family"`
	LocationBucket  string `json:"location_bucket"`
	CredentialRef   string `json:"credential_ref"`
	Enabled         bool   `json:"enabled"`
}

// PlanGCPWork returns one run and one work item per enabled configured GCP
// scope. Live collection must be explicitly enabled in the collector instance
// configuration; otherwise no claim-enabled GCP scheduling is admitted.
func (p GCPWorkPlanner) PlanGCPWork(
	_ context.Context,
	request GCPPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	if err := validateGCPPlanRequest(request); err != nil {
		return workflow.Run{}, nil, err
	}
	config, err := parseGCPRuntimeConfiguration(request.Instance.Configuration)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	scopes, err := gcpEnabledScopes(config)
	if err != nil {
		return workflow.Run{}, nil, err
	}
	scopes = filterGCPTargetsByScopeIDs(scopes, request.ScopeIDs)

	observedAt := request.ObservedAt.UTC()
	run := workflow.Run{
		RunID:              gcpRunID(request.Instance, request.PlanKey),
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  gcpRequestedScopeSet(request.Instance, scopes),
		RequestedCollector: string(scope.CollectorGCP),
		CreatedAt:          observedAt,
		UpdatedAt:          observedAt,
	}
	items := make([]workflow.WorkItem, 0, len(scopes))
	for _, target := range scopes {
		item, err := gcpWorkItem(request.Instance, target, run.RunID, request.PlanKey, observedAt)
		if err != nil {
			return workflow.Run{}, nil, err
		}
		items = append(items, item)
	}
	return run, items, nil
}

func validateGCPPlanRequest(request GCPPlanRequest) error {
	if err := request.Instance.Validate(); err != nil {
		return fmt.Errorf("gcp plan request: %w", err)
	}
	if request.Instance.CollectorKind != scope.CollectorGCP {
		return fmt.Errorf("gcp planner requires collector_kind %q", scope.CollectorGCP)
	}
	if !request.Instance.Enabled {
		return fmt.Errorf("gcp planner requires enabled collector instance")
	}
	if !request.Instance.ClaimsEnabled {
		return fmt.Errorf("gcp planner requires claim-enabled collector instance")
	}
	if request.ObservedAt.IsZero() {
		return fmt.Errorf("gcp planner observed_at must not be zero")
	}
	if err := validateSafePlanKey("gcp planner", request.PlanKey); err != nil {
		return err
	}
	return nil
}

func parseGCPRuntimeConfiguration(raw string) (gcpRuntimeConfiguration, error) {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		normalized = "{}"
	}
	var decoded gcpRuntimeConfiguration
	if err := json.Unmarshal([]byte(normalized), &decoded); err != nil {
		return gcpRuntimeConfiguration{}, fmt.Errorf("decode GCP collector configuration: %w", err)
	}
	for i := range decoded.Scopes {
		decoded.Scopes[i] = decoded.Scopes[i].withDefaults()
	}
	return decoded, nil
}

func validateGCPClaimSchedulerConfiguration(instance workflow.DesiredCollectorInstance) error {
	config, err := parseGCPRuntimeConfiguration(instance.Configuration)
	if err != nil {
		return fmt.Errorf("collector instance %q uses collector_kind %q: %w", instance.InstanceID, instance.CollectorKind, err)
	}
	if _, err := gcpEnabledScopes(config); err != nil {
		return fmt.Errorf("collector instance %q uses collector_kind %q: %w", instance.InstanceID, instance.CollectorKind, err)
	}
	return nil
}

func gcpEnabledScopes(config gcpRuntimeConfiguration) ([]gcpScopeConfiguration, error) {
	if !config.LiveCollectionEnabled {
		return nil, fmt.Errorf("claim-enabled GCP scheduling requires live_collection_enabled=true")
	}
	scopes := make([]gcpScopeConfiguration, 0, len(config.Scopes))
	seen := map[string]struct{}{}
	for i, target := range config.Scopes {
		if !target.Enabled {
			continue
		}
		if err := target.validate(); err != nil {
			return nil, fmt.Errorf("gcp scope[%d]: %w", i, err)
		}
		if _, ok := seen[target.ScopeID]; ok {
			return nil, fmt.Errorf("gcp scope[%d]: duplicate scope_id %q", i, target.ScopeID)
		}
		seen[target.ScopeID] = struct{}{}
		scopes = append(scopes, target)
	}
	sort.Slice(scopes, func(i, j int) bool {
		return scopes[i].ScopeID < scopes[j].ScopeID
	})
	if len(scopes) == 0 {
		return nil, fmt.Errorf("claim-enabled GCP scheduling requires at least one enabled scope")
	}
	return scopes, nil
}

func (s gcpScopeConfiguration) withDefaults() gcpScopeConfiguration {
	out := s
	out.ScopeID = strings.TrimSpace(out.ScopeID)
	out.ParentScopeKind = strings.TrimSpace(out.ParentScopeKind)
	out.ParentScopeID = strings.TrimSpace(out.ParentScopeID)
	out.AssetTypeFamily = firstNonEmpty(strings.TrimSpace(out.AssetTypeFamily), "mixed")
	out.ContentFamily = firstNonEmpty(strings.TrimSpace(out.ContentFamily), "resource")
	out.LocationBucket = firstNonEmpty(strings.TrimSpace(out.LocationBucket), "global")
	out.CredentialRef = strings.TrimSpace(out.CredentialRef)
	if out.ScopeID == "" {
		out.ScopeID = gcpruntime.DeriveScopeID(
			gcpcloud.ParentScopeKind(out.ParentScopeKind),
			out.ParentScopeID,
			out.AssetTypeFamily,
			out.ContentFamily,
			out.LocationBucket,
		)
	}
	return out
}

func (s gcpScopeConfiguration) validate() error {
	switch {
	case !gcpcloud.ParentScopeKind(s.ParentScopeKind).Valid():
		return fmt.Errorf("invalid parent_scope_kind %q", s.ParentScopeKind)
	case s.ParentScopeID == "":
		return fmt.Errorf("parent_scope_id is required")
	case strings.ContainsAny(s.ParentScopeID, ":/?#"):
		return fmt.Errorf("parent_scope_id contains unsupported path or query delimiters")
	case s.CredentialRef == "":
		return fmt.Errorf("credential_ref is required (read-only credential name)")
	case !validGCPScopeIDPart(s.AssetTypeFamily):
		return fmt.Errorf("asset_type_family is invalid")
	case !validGCPScopeIDPart(s.ContentFamily):
		return fmt.Errorf("content_family is invalid")
	case !validGCPScopeIDPart(s.LocationBucket):
		return fmt.Errorf("location_bucket is invalid")
	case s.ScopeID == "":
		return fmt.Errorf("scope_id could not be derived")
	default:
		return nil
	}
}

func validGCPScopeIDPart(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.ContainsAny(value, ":/?#")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func filterGCPTargetsByScopeIDs(
	targets []gcpScopeConfiguration,
	scopeIDs []string,
) []gcpScopeConfiguration {
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
	out := make([]gcpScopeConfiguration, 0, len(targets))
	for _, target := range targets {
		if _, ok := allowed[strings.TrimSpace(target.ScopeID)]; ok {
			out = append(out, target)
		}
	}
	return out
}

func gcpRequestedScopeSet(
	instance workflow.CollectorInstance,
	targets []gcpScopeConfiguration,
) string {
	type requestedTarget struct {
		ScopeID         string `json:"scope_id"`
		ParentScopeKind string `json:"parent_scope_kind"`
		ParentScopeID   string `json:"parent_scope_id"`
		AssetTypeFamily string `json:"asset_type_family"`
		ContentFamily   string `json:"content_family"`
		LocationBucket  string `json:"location_bucket"`
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
			ScopeID:         strings.TrimSpace(target.ScopeID),
			ParentScopeKind: strings.TrimSpace(target.ParentScopeKind),
			ParentScopeID:   strings.TrimSpace(target.ParentScopeID),
			AssetTypeFamily: strings.TrimSpace(target.AssetTypeFamily),
			ContentFamily:   strings.TrimSpace(target.ContentFamily),
			LocationBucket:  strings.TrimSpace(target.LocationBucket),
		})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func gcpRunID(instance workflow.CollectorInstance, planKey string) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s",
		scope.CollectorGCP,
		strings.TrimSpace(instance.InstanceID),
		workflow.TriggerKindSchedule,
		strings.TrimSpace(planKey),
	)
}

func gcpWorkItem(
	instance workflow.CollectorInstance,
	target gcpScopeConfiguration,
	runID string,
	planKey string,
	observedAt time.Time,
) (workflow.WorkItem, error) {
	scopeID := strings.TrimSpace(target.ScopeID)
	generationID := "gcp:" + facts.StableID("GCPWorkflowGeneration", map[string]any{
		"instance_id": strings.TrimSpace(instance.InstanceID),
		"plan_key":    strings.TrimSpace(planKey),
		"scope_id":    scopeID,
	})
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:%s", scope.CollectorGCP, strings.TrimSpace(instance.InstanceID), generationID),
		RunID:               runID,
		CollectorKind:       scope.CollectorGCP,
		CollectorInstanceID: strings.TrimSpace(instance.InstanceID),
		SourceSystem:        string(scope.CollectorGCP),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorGCP, strings.TrimSpace(instance.InstanceID), scopeID),
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           observedAt.UTC(),
		UpdatedAt:           observedAt.UTC(),
	}
	if err := item.Validate(); err != nil {
		return workflow.WorkItem{}, err
	}
	return item, nil
}
