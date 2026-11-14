// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestApplyRepoContextEvidenceDetailFullIsIdentity proves the HTTP default is
// byte-identical to an explicit full: the handles view never leaks into the
// default response (#10654).
func TestApplyRepoContextEvidenceDetailFullIsIdentity(t *testing.T) {
	t.Parallel()

	def := repoContextSizeBody()
	full := repoContextSizeBody()
	ApplyRepoContextEvidenceDetail(def, "")
	ApplyRepoContextEvidenceDetail(full, ContextEvidenceDetailFull)
	defBytes, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("marshal default: %v", err)
	}
	fullBytes, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal full: %v", err)
	}
	if string(defBytes) != string(fullBytes) {
		t.Fatal("default response differs from explicit full; want byte-identical")
	}
	// Full leaves the body untouched: no evidence_detail key, so the shaped
	// default is byte-identical to the pre-change body, not just to an
	// explicit full.
	for name, body := range map[string]map[string]any{"default": def, "full": full} {
		if _, ok := body["evidence_detail"]; ok {
			t.Fatalf("%s body sets evidence_detail = %v, want the key absent under full", name, body["evidence_detail"])
		}
	}
}

// TestApplyRepoContextEvidenceDetailHandlesMarksEveryCut proves every reduced
// family carries a truth omission naming the list and its original count.
func TestApplyRepoContextEvidenceDetailHandlesMarksEveryCut(t *testing.T) {
	t.Parallel()

	body := repoContextSizeBody()
	omissions := ApplyRepoContextEvidenceDetail(body, ContextEvidenceDetailHandles)
	if len(omissions) == 0 {
		t.Fatal("handles omissions empty; the fixture must exercise every projected family")
	}
	bySection := map[string]TruthOmission{}
	for _, omission := range omissions {
		bySection[omission.Section] = omission
	}
	for _, family := range RepoContextHandleListFamilies() {
		omission, ok := bySection[family]
		if !ok {
			t.Errorf("missing omission for family %q (got %v)", family, omissions)
			continue
		}
		if omission.Total <= 0 {
			t.Errorf("family %q total = %d, want > 0", family, omission.Total)
		}
	}
	if _, ok := body["evidence_detail_drilldown"]; !ok {
		t.Error("handles body missing evidence_detail_drilldown")
	}
	ev, ok := body["deployment_evidence"].(map[string]any)
	if !ok {
		t.Error("handles body missing deployment_evidence map")
	} else if _, ok := ev["evidence_index"]; ok {
		t.Error("handles body keeps deployment_evidence.evidence_index; handles must drop the regrouping")
	}
}

// TestApplyRepoContextEvidenceDetailUnknownIsInvalid proves unknown values
// are rejected by the shared validator before any read runs.
func TestApplyRepoContextEvidenceDetailUnknownIsInvalid(t *testing.T) {
	t.Parallel()

	if err := ValidateContextEvidenceDetail("compact"); err == nil {
		t.Fatal("ValidateContextEvidenceDetail(compact) = nil, want invalid_argument error")
	}
}

// TestRepoContextEvidenceDetailAtCapHandlesFitsBudget is the acceptance gate
// for #10654: the at-cap handles body must fit the 256 KiB dispatch budget.
func TestRepoContextEvidenceDetailAtCapHandlesFitsBudget(t *testing.T) {
	t.Parallel()

	const budget = 256 * 1024

	fullBody := repoContextSizeBody()
	fullBytes, err := json.Marshal(fullBody)
	if err != nil {
		t.Fatalf("marshal full: %v", err)
	}

	handlesBody := repoContextSizeBody()
	omissions := ApplyRepoContextEvidenceDetail(handlesBody, ContextEvidenceDetailHandles)
	if len(omissions) == 0 {
		t.Fatal("at-cap omissions empty; the fixture must exercise every projected family")
	}
	handlesBytes, err := json.Marshal(handlesBody)
	if err != nil {
		t.Fatalf("marshal handles: %v", err)
	}

	t.Logf("repo context full body = %d bytes, handles body = %d bytes, budget = %d bytes", len(fullBytes), len(handlesBytes), budget)
	if len(fullBytes) <= budget {
		t.Fatalf("full body = %d bytes, want over the %d budget so the fixture proves the defect", len(fullBytes), budget)
	}
	if len(handlesBytes) >= len(fullBytes) {
		t.Fatalf("handles body = %d bytes, want less than full body = %d", len(handlesBytes), len(fullBytes))
	}
	if len(handlesBytes) > budget {
		t.Fatalf("handles body = %d bytes, want <= %d budget", len(handlesBytes), budget)
	}
}

// TestApplyRepoContextEvidenceDetailEmptyIsValid proves empty, nil, and
// degraded inputs each return a valid shaped body.
func TestApplyRepoContextEvidenceDetailEmptyIsValid(t *testing.T) {
	t.Parallel()

	for _, body := range []map[string]any{
		{},
		{"relationships": []map[string]any{}, "partial_reasons": []string{"relationships_read_degraded"}},
		{"relationship_overview": map[string]any{}, "partial_reasons": []string{"relationship_overview_read_degraded"}},
	} {
		omissions := ApplyRepoContextEvidenceDetail(body, ContextEvidenceDetailHandles)
		if len(omissions) != 0 {
			t.Fatalf("empty body omissions = %v, want none", omissions)
		}
		if got := StringVal(body, "evidence_detail"); got != ContextEvidenceDetailHandles {
			t.Fatalf("evidence_detail = %q, want handles", got)
		}
	}
}

const repoContextSizeProse = "a wide evidence body describing how this row was established, " +
	"with enough detail to stand in for production prose, matched values, and payloads. "

func repoContextSizeWideRow(identity map[string]any, dropped int) map[string]any {
	row := map[string]any{}
	for k, v := range identity {
		row[k] = v
	}
	for i := range dropped {
		row["dropped_prose_"+string(rune('a'+i%26))+string(rune('0'+i%10))] = strings.Repeat(repoContextSizeProse, 3)
	}
	return row
}

// repoContextEvidenceIndexBucket builds one production-shaped evidence_index
// bucket with per-artifact id lists, mirroring finalizeDeploymentEvidenceIndex.
func repoContextEvidenceIndexBucket(prefix string, n int) map[string]any {
	resolved := make([]string, 0, n)
	generations := make([]string, 0, n)
	for i := range n {
		suffix := string(rune('a'+i%26)) + string(rune('0'+(i/26)%10))
		resolved = append(resolved, prefix+"-res-"+suffix)
		generations = append(generations, prefix+"-gen-"+suffix)
	}
	return map[string]any{
		"artifact_count":    n,
		"resolved_ids":      resolved,
		"generation_ids":    generations,
		"evidence_kinds":    []string{"helm_release"},
		"artifact_families": []string{prefix},
	}
}

// repoContextSizeBody builds an at-cap repository context: 200 wide
// relationship rows with partition repeats, 100 wide dependency rows, 200
// consumers, 100 wide API endpoints, 100 wide deployment evidence artifacts
// with a production-shaped evidence_index regrouping, 100 wide content
// artifacts in production map shape (50 controller plus 50 runtime rows),
// 100 entry points, and 200 infrastructure rows.
func repoContextSizeBody() map[string]any {
	relationships := make([]map[string]any, 0, 200)
	for range 200 {
		relationships = append(relationships, repoContextSizeWideRow(map[string]any{
			"type": "DEPENDS_ON", "direction": "outgoing",
			"source_name": "edge-api", "source_id": "repo-edge",
			"target_name": "dep", "target_id": "repo-dep",
			"evidence_type": "go_mod_require", "resolved_id": "res-wide",
			"confidence": 0.8, "evidence_count": 5,
		}, 4))
	}
	dependencies := make([]map[string]any, 0, 100)
	for range 100 {
		dependencies = append(dependencies, repoContextSizeWideRow(map[string]any{
			"type": "DEPENDS_ON", "target_name": "dep", "target_id": "repo-dep",
			"evidence_type": "go_mod_require", "resolved_id": "res-wide",
		}, 5))
	}
	consumers := make([]map[string]any, 0, 200)
	for i := range 200 {
		consumers = append(consumers, map[string]any{
			"name": "consumer",
			"id":   "repo-consumer",
		})
		_ = i
	}
	endpoints := make([]map[string]any, 0, 100)
	for range 100 {
		endpoints = append(endpoints, repoContextSizeWideRow(map[string]any{
			"id": "ep-wide", "path": "/api/v0/wide", "methods": []string{"GET"},
		}, 6))
	}
	evidenceArtifacts := make([]map[string]any, 0, 100)
	for range 100 {
		evidenceArtifacts = append(evidenceArtifacts, repoContextSizeWideRow(map[string]any{
			"id": "art-wide", "path": "charts/api", "evidence_kind": "helm_release",
			"artifact_family": "helm", "relationship_type": "DEPLOYS_FROM",
			"source_repo_id": "repo-edge", "target_repo_id": "repo-platform",
			"resolved_id": "res-wide",
		}, 6))
	}
	controllerArtifacts := make([]map[string]any, 0, 50)
	for range 50 {
		controllerArtifacts = append(controllerArtifacts, repoContextSizeWideRow(map[string]any{
			"path": "Jenkinsfile", "controller_kind": "jenkins_pipeline",
			"artifact_name": "deploy-pipeline", "service_name": "edge-api",
		}, 8))
	}
	runtimeArtifacts := make([]map[string]any, 0, 50)
	for range 50 {
		runtimeArtifacts = append(runtimeArtifacts, repoContextSizeWideRow(map[string]any{
			"relative_path": "Dockerfile.svc", "artifact_type": "dockerfile",
			"artifact_name": "api-image", "service_name": "edge-api",
		}, 8))
	}
	// Production stores deployment_artifacts as a map of named lists
	// (LoadDeploymentArtifactOverviewWithStats merges controller, runtime,
	// workflow, and CloudFormation maps); the fixture mirrors that shape so
	// the at-cap test proves the shaper descends into it.
	contentArtifacts := map[string]any{
		"controller_artifacts": controllerArtifacts,
		"deployment_artifacts": runtimeArtifacts,
	}
	entryPoints := make([]map[string]any, 0, 100)
	for range 100 {
		entryPoints = append(entryPoints, repoContextSizeWideRow(map[string]any{
			"name": "main", "relative_path": "cmd/api/main.go", "language": "go",
		}, 4))
	}
	infra := make([]map[string]any, 0, 200)
	for range 200 {
		// Production identity is type|name|file_path
		// (repositoryInfrastructureEntryKey); kind and source are the real
		// optional classifiers.
		infra = append(infra, repoContextSizeWideRow(map[string]any{
			"type": "K8sResource", "name": "edge-api",
			"file_path": "charts/api/templates/deploy.yaml",
			"kind":      "Deployment", "source": "helm",
		}, 4))
	}
	relationshipOverview := map[string]any{
		"relationship_count": 200,
		"relationships":      relationships,
		"controller_driven":  relationships,
		"workflow_driven":    relationships,
		"story":              "Depends on many.",
	}
	return map[string]any{
		"repository":            map[string]any{"id": "repo-edge"},
		"relationships":         dependencies,
		"relationship_overview": relationshipOverview,
		"consumers":             consumers,
		"api_surface": map[string]any{
			"endpoint_count": 100, "endpoints": endpoints,
		},
		"deployment_evidence": map[string]any{
			"artifact_count": 100, "artifacts": evidenceArtifacts,
			"evidence_index": map[string]any{
				"lookup_basis": "resolved_id",
				"relationship_types": map[string]any{
					"depends_on": repoContextEvidenceIndexBucket("dep", 60),
					"consumes":   repoContextEvidenceIndexBucket("con", 40),
				},
				"artifact_families": map[string]any{
					"helm":      repoContextEvidenceIndexBucket("helm", 50),
					"terraform": repoContextEvidenceIndexBucket("tf", 50),
				},
				"evidence_kinds": map[string]any{
					"sbom":       repoContextEvidenceIndexBucket("sbom", 70),
					"provenance": repoContextEvidenceIndexBucket("prov", 30),
				},
			},
		},
		"deployment_artifacts": contentArtifacts,
		"infrastructure_overview": map[string]any{
			"deployment_artifacts": contentArtifacts,
		},
		"entry_points":    entryPoints,
		"infrastructure":  infra,
		"partial_reasons": []string{},
	}
}
