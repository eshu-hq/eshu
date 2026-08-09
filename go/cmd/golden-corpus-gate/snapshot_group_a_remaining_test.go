// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

type groupARemainingExpectation struct {
	slug          string
	key           string
	http          bool
	arguments     map[string]any
	minimum       int
	maximum       int
	resultsField  string
	required      []string
	values        map[string]any
	objectMatches map[string][]map[string]any
}

func TestGoldenSnapshotGroupARemainingCapabilitiesAreNonVacuous(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	goRepo := groupACanonicalRepoID(t, "go_comprehensive")
	dartRepo := groupACanonicalRepoID(t, "dart_comprehensive")
	jsRepo := groupACanonicalRepoID(t, "javascript_comprehensive")
	const (
		ciRepo       = "repository:r_69256c06"
		ciScope      = "ci_cd_run:github_actions:eshu-hq:supply-chain-demo"
		ciGeneration = "cassette-cicd-scd-gen2-artifact"
		imageDigest  = "sha256:0000000000000000000000000000000000000000000000000000000000aa"
	)

	tests := []groupARemainingExpectation{
		{
			slug: "prod-admission-decisions", key: "list_admission_decisions", minimum: 1, maximum: 1, resultsField: "decisions",
			arguments:     map[string]any{"domain": "ci_cd_run_correlation", "scope_id": ciScope, "generation_id": ciGeneration, "anchor_kind": "repository", "anchor_id": ciRepo, "include_evidence": false, "limit": float64(10)},
			required:      []string{"decisions", "count", "limit", "truncated", "recommended_next_calls"},
			values:        map[string]any{"count": float64(1), "limit": float64(10), "truncated": false},
			objectMatches: map[string][]map[string]any{"decisions[]": {{"domain": "ci_cd_run_correlation", "scope_id": ciScope, "generation_id": ciGeneration, "anchor_kind": "repository", "anchor_id": ciRepo, "state": "admitted"}}},
		},
		{
			slug: "prod-advisory-catalog", key: "GET /api/v0/supply-chain/advisories?limit=10&q=CVE-2026-00010", http: true, minimum: 1, maximum: 1, resultsField: "advisories",
			required: []string{"advisories", "count", "limit", "scope", "truncated"},
			values:   map[string]any{"count": float64(1), "limit": float64(10), "scope.q": "CVE-2026-00010", "truncated": false, "advisories[].advisory_key": "CVE-2026-00010", "advisories[].canonical_id": "CVE-2026-00010", "advisories[].cve_id": "CVE-2026-00010", "advisories[].severity_label": "HIGH", "advisories[].cvss_score": float64(7.5)},
		},
		{
			slug: "prod-answer-narration-status", key: "get_answer_narration_status",
			required: []string{"state", "reason", "deterministic_fallback_available", "canonical_truth_affected", "retention_posture"},
			values:   map[string]any{"state": "unavailable", "reason": "disabled_by_default", "deterministic_fallback_available": true, "canonical_truth_affected": false, "retention_posture": "metadata_only"},
		},
		{
			slug: "prod-call-chain-path", key: "find_function_call_chain", minimum: 1, maximum: 1, resultsField: "chains",
			arguments: map[string]any{"start": "recursionFib", "end": "recursionFib", "repo_id": dartRepo, "max_depth": float64(2)},
			required:  []string{"start", "end", "repo_id", "cross_repo", "chains"},
			values:    map[string]any{"start": "recursionFib", "end": "recursionFib", "repo_id": dartRepo, "cross_repo": false, "chains[].chain[].name": "recursionFib"},
		},
		{
			slug: "prod-catalog", key: "GET /api/v0/catalog?limit=2000&offset=0", http: true, minimum: 1, maximum: 2000, resultsField: "repositories",
			required: []string{"repositories", "workloads", "services", "counts", "count", "limit", "truncated", "workloads_truncated"},
			values:   map[string]any{"limit": float64(2000), "repositories[].id": goRepo, "workloads[].id": "workload:deployable-config", "services[].id": "component:default/deployable-config"},
		},
		{
			slug: "prod-ci-cd-run-correlation-aggregate", key: "count_ci_cd_run_correlations",
			arguments: map[string]any{"repository_id": ciRepo, "provider": "github_actions", "run_id": "5151", "outcome": "derived"},
			required:  []string{"total_correlations", "by_outcome", "by_environment", "by_provider", "scope"},
			values:    map[string]any{"total_correlations": float64(1), "by_outcome.derived": float64(1), "by_provider.github_actions": float64(1), "scope.repository_id": ciRepo, "scope.provider": "github_actions", "scope.run_id": "5151", "scope.outcome": "derived"},
		},
		{
			slug: "prod-class-methods", key: "analyze_code_relationships",
			arguments: map[string]any{"target": "Dog", "query_type": "class_hierarchy", "repo_id": jsRepo, "max_depth": float64(4), "limit": float64(10)},
			required:  []string{"coverage", "scope", "source_backend", "target_resolution", "class_hierarchy"},
			values:    map[string]any{"coverage.query_shape": "entity_anchor_class_hierarchy_story", "target_resolution.status": "resolved", "target_resolution.candidates[].name": "Dog", "class_hierarchy.methods[].method_name": "fetch", "class_hierarchy.parents[].target_name": "Animal"},
		},
		{
			slug: "prod-code-quality-refactoring", key: "inspect_code_quality", minimum: 1, maximum: 1, resultsField: "results",
			arguments: map[string]any{"check": "complexity", "repo_id": goRepo, "language": "go", "function_name": "GoldenDataflowHandler", "min_complexity": float64(2), "limit": float64(1), "offset": float64(0)},
			required:  []string{"check", "repo_id", "language", "results", "source_backend", "truncated"},
			values:    map[string]any{"check": "complexity", "repo_id": goRepo, "language": "go", "results[].name": "GoldenDataflowHandler", "results[].file_path": "dataflow_proof.go", "results[].complexity": float64(2)},
		},
		{
			slug: "prod-code-search-fuzzy", key: "find_code", minimum: 1, maximum: 1, resultsField: "matches",
			arguments: map[string]any{"query": "GoldenDataflow", "repo_id": goRepo, "exact": false, "limit": float64(1)},
			required:  []string{"matches", "query", "repo_id", "count", "limit", "truncated", "source_backend"},
			values:    map[string]any{"query": "GoldenDataflow", "repo_id": goRepo, "count": float64(1), "limit": float64(1), "matches[].name": "GoldenDataflowHandler", "matches[].file_path": "dataflow_proof.go", "matches[].labels[]": "Function"},
		},
		{
			slug: "prod-complexity", key: "calculate_cyclomatic_complexity", minimum: 1, maximum: 1, resultsField: "results",
			arguments: map[string]any{"function_name": "GoldenDataflowHandler", "repo_id": goRepo},
			required:  []string{"limit", "repo_id", "result_key", "results", "truncated"},
			values:    map[string]any{"repo_id": goRepo, "results[].name": "GoldenDataflowHandler", "results[].file_path": "dataflow_proof.go", "results[].complexity": float64(2)},
		},
		{
			slug: "prod-container-image-identity-aggregate", key: "count_container_image_identities",
			arguments: map[string]any{"digest": imageDigest},
			required:  []string{"total_identities", "by_outcome", "by_identity_strength", "scope"},
			values:    map[string]any{"total_identities": float64(1), "by_identity_strength.explicit_digest": float64(1), "scope.digest": imageDigest},
		},
		{
			slug: "prod-content-search", key: "search_file_content", minimum: 1, maximum: 1, resultsField: "matches",
			arguments: map[string]any{"query": "GoldenDataflowHandler", "repo_id": goRepo, "limit": float64(1)},
			required:  []string{"matches", "results", "count", "limit", "offset", "truncated", "source_backend"},
			values:    map[string]any{"count": float64(1), "limit": float64(1), "offset": float64(0), "source_backend": "postgres_content_store", "matches[].repo_id": goRepo, "matches[].relative_path": "dataflow_proof.go"},
		},
	}

	for _, test := range tests {
		t.Run(test.slug, func(t *testing.T) {
			shape, ok := groupARemainingShape(snapshot, test)
			if !ok {
				t.Fatalf("snapshot shape %q is missing", test.key)
			}
			if test.arguments != nil && !reflect.DeepEqual(shape.Arguments, test.arguments) {
				t.Errorf("arguments = %#v, want %#v", shape.Arguments, test.arguments)
			}
			if test.resultsField != "" && (shape.MinimumResults != test.minimum || shape.MaximumResults != test.maximum || shape.ResultsField != test.resultsField) {
				t.Errorf("bounds/results = [%d,%d] %q, want [%d,%d] %q", shape.MinimumResults, shape.MaximumResults, shape.ResultsField, test.minimum, test.maximum, test.resultsField)
			}
			assertSnapshotPaths(t, shape.RequiredResponseFields, test.required)
			assertSnapshotValues(t, shape.RequiredJSONValues, test.values)
			for path, want := range test.objectMatches {
				if got := shape.RequiredJSONObjectMatches[path]; !reflect.DeepEqual(got, want) {
					t.Errorf("object matches %q = %#v, want %#v", path, got, want)
				}
			}
		})
	}
}

func TestGoldenSnapshotGroupARemainingBITES(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		shape QueryShape
		body  string
	}{
		"advisory-empty":        {snapshot.QueryShapes.HTTP["GET /api/v0/supply-chain/advisories?limit=10&q=CVE-2026-00010"], `{"advisories":[],"count":0,"limit":10,"scope":{"q":"CVE-2026-00010"},"truncated":false}`},
		"narration-wrong-state": {snapshot.QueryShapes.MCP["get_answer_narration_status"], `{"state":"available","reason":"available","deterministic_fallback_available":true,"canonical_truth_affected":false,"retention_posture":"metadata_only"}`},
		"content-empty":         {snapshot.QueryShapes.MCP["search_file_content"], `{"matches":[],"results":[],"count":0,"limit":1,"offset":0,"truncated":false,"source_backend":"postgres_content_store"}`},
	} {
		if finding := EvaluateQueryShape(name, test.shape, []byte(test.body)); finding.OK {
			t.Errorf("seeded wrong/empty response passed: %+v", finding)
		}
	}
}

func groupARemainingShape(snapshot Snapshot, expectation groupARemainingExpectation) (QueryShape, bool) {
	if expectation.http {
		shape, ok := snapshot.QueryShapes.HTTP[expectation.key]
		return shape, ok
	}
	shape, ok := snapshot.QueryShapes.MCP[expectation.key]
	return shape, ok
}

func groupACanonicalRepoID(t *testing.T, fixture string) string {
	t.Helper()
	id, err := repositoryidentity.CanonicalRepositoryID("https://github.com/acme/"+fixture, "")
	if err != nil {
		t.Fatalf("canonical repository ID for %s: %v", fixture, err)
	}
	return id
}
