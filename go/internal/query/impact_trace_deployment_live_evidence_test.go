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

	// Live evidence must produce 0.95 confidence and the
	// live_runtime_observation reason string.
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

	// Live evidence must return live_runtime_observation even when
	// config-materialized instances are present. The live tier is
	// stronger and the legacy reason must NOT return.
	instances := []map[string]any{
		{"materialization_confidence": 0.9},
	}
	confidence, reason := deploymentOverallConfidence(instances, nil, nil, true)
	if confidence != 0.95 {
		t.Fatalf("deploymentOverallConfidence(live=true, instances) confidence = %v, want 0.95", confidence)
	}
	if reason != "live_runtime_observation" {
		t.Fatalf("deploymentOverallConfidence(live=true, instances) reason = %q, want %q", reason, "live_runtime_observation")
	}
}

func TestDeploymentOverallConfidenceNoEvidence(t *testing.T) {
	t.Parallel()

	// No evidence (no instances, no deployment sources, no config
	// environments, no live evidence) must stay at 0/no_deployment_evidence.
	confidence, reason := deploymentOverallConfidence(nil, nil, nil, false)
	if confidence != 0 {
		t.Fatalf("deploymentOverallConfidence(no evidence) confidence = %v, want 0", confidence)
	}
	if reason != "no_deployment_evidence" {
		t.Fatalf("deploymentOverallConfidence(no evidence) reason = %q, want %q", reason, "no_deployment_evidence")
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
		false, // no live evidence
	)
	// Config-materialized instances exist, so tier must be config_only.
	if tier, ok := summary["deployment_truth_tier"]; !ok {
		t.Fatal("deployment_truth_tier missing from summary when instances present")
	} else if tier != "config_only" {
		t.Fatalf("deployment_truth_tier = %q, want %q", tier, "config_only")
	}
	// Legacy reason must be preserved.
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
		false, // no live evidence
	)
	// When no evidence at all (no instances, no deployment sources, no
	// config environments, no live evidence), the tier must be absent
	// from the summary.
	if _, ok := summary["deployment_truth_tier"]; ok {
		t.Fatal("deployment_truth_tier must be absent when no evidence exists")
	}
	if confidence := summary["overall_confidence"]; confidence != 0.0 {
		t.Fatalf("overall_confidence = %v, want 0", confidence)
	}
}

func TestFetchWorkloadLiveEvidenceNoGraph(t *testing.T) {
	t.Parallel()

	// A nil handler or nil graph must return false, no error.
	h := &ImpactHandler{}
	live, err := h.fetchWorkloadLiveEvidence(t.Context(), "workload:test")
	if err != nil {
		t.Fatalf("fetchWorkloadLiveEvidence(nil graph) error = %v, want nil", err)
	}
	if live {
		t.Fatal("fetchWorkloadLiveEvidence(nil graph) = true, want false")
	}
}

func TestFetchWorkloadLiveEvidenceEmptyID(t *testing.T) {
	t.Parallel()

	// Empty workload id must return false, no error without calling the
	// graph at all.
	h := &ImpactHandler{Neo4j: &recordingGraphQuery{}}
	live, err := h.fetchWorkloadLiveEvidence(t.Context(), "")
	if err != nil {
		t.Fatalf("fetchWorkloadLiveEvidence(empty id) error = %v, want nil", err)
	}
	if live {
		t.Fatal("fetchWorkloadLiveEvidence(empty id) = true, want false")
	}
}

// recordingGraphQuery implements GraphQuery by returning the rows set in the
// test. It records the last query and params for assertion.
type recordingGraphQuery struct {
	rows       []map[string]any
	err        error
	lastCypher string
	lastParams map[string]any
}

func (r *recordingGraphQuery) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.lastCypher = cypher
	r.lastParams = params
	return r.rows, r.err
}

func (r *recordingGraphQuery) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, nil
}

func TestFetchWorkloadLiveEvidenceTrue(t *testing.T) {
	t.Parallel()

	fake := &recordingGraphQuery{
		rows: []map[string]any{
			{"has_live_evidence": true},
		},
	}
	h := &ImpactHandler{Neo4j: fake}
	live, err := h.fetchWorkloadLiveEvidence(t.Context(), "workload:test")
	if err != nil {
		t.Fatalf("fetchWorkloadLiveEvidence error = %v, want nil", err)
	}
	if !live {
		t.Fatal("fetchWorkloadLiveEvidence = false, want true")
	}
}

func TestFetchWorkloadLiveEvidenceFalse(t *testing.T) {
	t.Parallel()

	fake := &recordingGraphQuery{
		rows: []map[string]any{
			{"has_live_evidence": false},
		},
	}
	h := &ImpactHandler{Neo4j: fake}
	live, err := h.fetchWorkloadLiveEvidence(t.Context(), "workload:test")
	if err != nil {
		t.Fatalf("fetchWorkloadLiveEvidence error = %v, want nil", err)
	}
	if live {
		t.Fatal("fetchWorkloadLiveEvidence = true, want false")
	}
}

func TestFetchWorkloadLiveEvidenceEmptyRows(t *testing.T) {
	t.Parallel()

	fake := &recordingGraphQuery{
		rows: nil, // no rows returned
	}
	h := &ImpactHandler{Neo4j: fake}
	live, err := h.fetchWorkloadLiveEvidence(t.Context(), "workload:test")
	if err != nil {
		t.Fatalf("fetchWorkloadLiveEvidence error = %v, want nil", err)
	}
	if live {
		t.Fatal("fetchWorkloadLiveEvidence = true, want false (empty rows)")
	}
}

func TestFetchWorkloadLiveEvidenceError(t *testing.T) {
	t.Parallel()

	fake := &recordingGraphQuery{
		err: fmt.Errorf("graph offline"),
	}
	h := &ImpactHandler{Neo4j: fake}
	_, err := h.fetchWorkloadLiveEvidence(t.Context(), "workload:test")
	if err == nil {
		t.Fatal("fetchWorkloadLiveEvidence error = nil, want non-nil")
	}
}
