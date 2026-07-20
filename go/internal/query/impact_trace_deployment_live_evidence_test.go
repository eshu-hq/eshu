// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"testing"
)

func TestDeploymentOverallConfidenceLiveEvidence(t *testing.T) {
	t.Parallel()

	confidence, reason := deploymentOverallConfidence(nil, nil, nil, true)
	if confidence != 0.95 {
		t.Fatalf("deploymentOverallConfidence(live=true) confidence = %v, want 0.95", confidence)
	}
	if reason != "live_runtime_observation" {
		t.Fatalf("deploymentOverallConfidence(live=true) reason = %q, want %q", reason, "live_runtime_observation")
	}
}

func TestDeploymentOverallConfidenceLiveEvidenceOverridesInstances(t *testing.T) {
	t.Parallel()

	instances := []map[string]any{
		{"materialization_confidence": 0.9},
	}
	confidence, reason := deploymentOverallConfidence(instances, nil, nil, true)
	if confidence != 0.95 {
		t.Fatalf("confidence = %v, want 0.95", confidence)
	}
	if reason != "live_runtime_observation" {
		t.Fatalf("reason = %q, want %q", reason, "live_runtime_observation")
	}
}

func TestDeploymentOverallConfidenceNoEvidence(t *testing.T) {
	t.Parallel()

	confidence, reason := deploymentOverallConfidence(nil, nil, nil, false)
	if confidence != 0 {
		t.Fatalf("confidence = %v, want 0", confidence)
	}
	if reason != "no_deployment_evidence" {
		t.Fatalf("reason = %q, want %q", reason, "no_deployment_evidence")
	}
}

func TestBuildDeploymentFactSummaryTierLiveEvidence(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()
	instances, _ := ctx["instances"].([]map[string]any)
	summary := buildDeploymentFactSummary(
		ctx,
		instances,
		[]string{"production", "qa"},
		nil,
		[]string{"eks-prod", "ecs-qa"},
		nil,
		nil,
		nil,
		nil,
		nil,
		"controller",
		true, // hasLiveEvidence
	)
	if tier, ok := summary["deployment_truth_tier"]; !ok {
		t.Fatal("deployment_truth_tier missing from summary")
	} else if tier != "runtime_confirmed" {
		t.Fatalf("deployment_truth_tier = %q, want %q", tier, "runtime_confirmed")
	}
	if confidence := summary["overall_confidence"]; confidence != 0.95 {
		t.Fatalf("overall_confidence = %v, want 0.95", confidence)
	}
	if reason := summary["overall_confidence_reason"]; reason != "live_runtime_observation" {
		t.Fatalf("overall_confidence_reason = %q, want %q", reason, "live_runtime_observation")
	}
}

func TestBuildDeploymentFactSummaryTierConfigOnly(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()
	instances, _ := ctx["instances"].([]map[string]any)
	summary := buildDeploymentFactSummary(
		ctx,
		instances,
		[]string{"production", "qa"},
		nil,
		[]string{"eks-prod", "ecs-qa"},
		nil,
		nil,
		nil,
		nil,
		nil,
		"controller",
		false,
	)
	if tier, ok := summary["deployment_truth_tier"]; !ok {
		t.Fatal("deployment_truth_tier missing")
	} else if tier != "config_only" {
		t.Fatalf("deployment_truth_tier = %q, want %q", tier, "config_only")
	}
	if reason := summary["overall_confidence_reason"]; reason != "materialized_runtime_instances" {
		t.Fatalf("overall_confidence_reason = %q, want %q", reason, "materialized_runtime_instances")
	}
}

func TestBuildDeploymentFactSummaryTierEmptyWhenNoEvidence(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{}
	summary := buildDeploymentFactSummary(
		ctx,
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
		"",
		false,
	)
	if _, ok := summary["deployment_truth_tier"]; ok {
		t.Fatal("deployment_truth_tier must be absent when no evidence exists")
	}
}

// stubKubernetesCorrelationStore is a test fake that returns matching
// rows for filters whose ImageRef and Outcome match. It implements
// KubernetesCorrelationStore.
type stubKubernetesCorrelationStore struct {
	rows []KubernetesCorrelationRow
	err  error
	// lastFilter records the filter passed to the most recent call so
	// tests can assert access-scoping fields.
	lastFilter KubernetesCorrelationFilter
}

func (s *stubKubernetesCorrelationStore) ListKubernetesCorrelations(
	_ context.Context,
	filter KubernetesCorrelationFilter,
) ([]KubernetesCorrelationRow, error) {
	s.lastFilter = filter
	if s.err != nil {
		return nil, s.err
	}
	// Filter stably: only return rows whose ImageRef and Outcome match
	// the filter when those fields are populated, mirroring the real
	// Postgres store's WHERE payload->>'image_ref' = $6 AND
	// payload->>'outcome' = $8 predicates.
	var matched []KubernetesCorrelationRow
	for _, row := range s.rows {
		if filter.ImageRef != "" && row.ImageRef != filter.ImageRef {
			continue
		}
		if filter.Outcome != "" && row.Outcome != filter.Outcome {
			continue
		}
		matched = append(matched, row)
	}
	return matched, nil
}

func TestFetchWorkloadLiveEvidenceNilHandler(t *testing.T) {
	t.Parallel()

	var h *ImpactHandler
	live, err := h.fetchWorkloadLiveEvidence(t.Context(), nil, repositoryAccessFilter{})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if live {
		t.Fatal("nil handler returned true, want false")
	}
}

func TestFetchWorkloadLiveEvidenceNilStore(t *testing.T) {
	t.Parallel()

	h := &ImpactHandler{} // KubernetesCorrelations is nil
	live, err := h.fetchWorkloadLiveEvidence(t.Context(), []string{"img:latest"}, repositoryAccessFilter{allScopes: true})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if live {
		t.Fatal("nil store returned true, want false")
	}
}

func TestFetchWorkloadLiveEvidenceEmptyImageRefs(t *testing.T) {
	t.Parallel()

	store := &stubKubernetesCorrelationStore{}
	h := &ImpactHandler{KubernetesCorrelations: store}
	live, err := h.fetchWorkloadLiveEvidence(t.Context(), nil, repositoryAccessFilter{allScopes: true})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if live {
		t.Fatal("empty image_refs returned true, want false")
	}
}

func TestFetchWorkloadLiveEvidenceExactMatch(t *testing.T) {
	t.Parallel()

	matchingImage := "ghcr.io/eshu-hq/supply-chain-demo@sha256:abcdef"
	store := &stubKubernetesCorrelationStore{
		rows: []KubernetesCorrelationRow{
			{ImageRef: matchingImage, Outcome: "exact"},
		},
	}
	h := &ImpactHandler{KubernetesCorrelations: store}
	live, err := h.fetchWorkloadLiveEvidence(
		t.Context(),
		[]string{"other:img@sha256:1111", matchingImage},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if !live {
		t.Fatal("exact match returned false, want true")
	}
}

func TestFetchWorkloadLiveEvidenceExactNoMatch(t *testing.T) {
	t.Parallel()

	store := &stubKubernetesCorrelationStore{
		rows: []KubernetesCorrelationRow{
			{ImageRef: "different:img@sha256:9999", Outcome: "exact"},
		},
	}
	h := &ImpactHandler{KubernetesCorrelations: store}
	live, err := h.fetchWorkloadLiveEvidence(
		t.Context(),
		[]string{"workload:img@sha256:1111"},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if live {
		t.Fatal("non-matching image_ref returned true, want false")
	}
}

func TestFetchWorkloadLiveEvidenceDerivedOutcomeNotExact(t *testing.T) {
	t.Parallel()

	img := "app:img@sha256:abc"
	store := &stubKubernetesCorrelationStore{
		rows: []KubernetesCorrelationRow{
			{ImageRef: img, Outcome: "derived"},
		},
	}
	h := &ImpactHandler{KubernetesCorrelations: store}
	live, err := h.fetchWorkloadLiveEvidence(
		t.Context(),
		[]string{img},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if live {
		t.Fatal("derived outcome must not count as runtime_confirmed")
	}
}

func TestFetchWorkloadLiveEvidenceAmbiguousOutcomeNotExact(t *testing.T) {
	t.Parallel()

	img := "app:img@sha256:abc"
	store := &stubKubernetesCorrelationStore{
		rows: []KubernetesCorrelationRow{
			{ImageRef: img, Outcome: "ambiguous"},
		},
	}
	h := &ImpactHandler{KubernetesCorrelations: store}
	live, err := h.fetchWorkloadLiveEvidence(
		t.Context(),
		[]string{img},
		repositoryAccessFilter{allScopes: true},
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if live {
		t.Fatal("ambiguous outcome must not count as runtime_confirmed")
	}
}

func TestFetchWorkloadLiveEvidenceStoreError(t *testing.T) {
	t.Parallel()

	store := &stubKubernetesCorrelationStore{
		err: fmt.Errorf("postgres offline"),
	}
	h := &ImpactHandler{KubernetesCorrelations: store}
	_, err := h.fetchWorkloadLiveEvidence(
		t.Context(),
		[]string{"img:latest"},
		repositoryAccessFilter{allScopes: true},
	)
	if err == nil {
		t.Fatal("store error must be surfaced, got nil")
	}
}

func TestFetchWorkloadLiveEvidenceScopedAccessFilter(t *testing.T) {
	t.Parallel()

	store := &stubKubernetesCorrelationStore{
		rows: []KubernetesCorrelationRow{
			{ImageRef: "img@sha256:a", Outcome: "exact"},
		},
	}
	h := &ImpactHandler{KubernetesCorrelations: store}
	access := repositoryAccessFilter{
		allScopes:            false,
		allowedRepositoryIDs: []string{"repo:sample-service-api"},
		allowedScopeIDs:      []string{"scope:test"},
		allowed: map[string]struct{}{
			"repo:sample-service-api": {},
			"scope:test":              {},
		},
	}
	live, err := h.fetchWorkloadLiveEvidence(
		t.Context(),
		[]string{"img@sha256:a"},
		access,
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if !live {
		t.Fatal("exact match with scoped access returned false")
	}
	// The store must have received the access-scoping fields.
	if store.lastFilter.AllScopes {
		t.Fatal("AllScopes must be false for scoped access")
	}
	if len(store.lastFilter.AllowedRepositoryIDs) == 0 {
		t.Fatal("AllowedRepositoryIDs must be populated for scoped access")
	}
	if len(store.lastFilter.AllowedScopeIDs) == 0 {
		t.Fatal("AllowedScopeIDs must be populated for scoped access")
	}
}

func TestFetchWorkloadLiveEvidenceEmptyAccess(t *testing.T) {
	t.Parallel()

	// An empty access filter (no grants, not all-scopes) means a scoped
	// caller with zero grants. The store must never be called.
	store := &stubKubernetesCorrelationStore{
		rows: []KubernetesCorrelationRow{
			{ImageRef: "img@sha256:a", Outcome: "exact"},
		},
	}
	h := &ImpactHandler{KubernetesCorrelations: store}
	live, err := h.fetchWorkloadLiveEvidence(
		t.Context(),
		[]string{"img@sha256:a"},
		repositoryAccessFilter{}, // empty: no grants, scoped
	)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if live {
		t.Fatal("empty access filter must return false without querying")
	}
}
