// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"encoding/json"
	"reflect"
	"testing"
)

// repoStoryDetailBody builds a populated repository-story response exercising
// every family ApplyRepoStoryEvidenceDetail projects: relationship rows plus
// their partition repeats, graph deployment evidence with its index, content
// deployment artifacts in both overview locations, delivery lists, target
// documentation findings, target support evidence, and framework rows.
func repoStoryDetailBody() map[string]any {
	relationship := map[string]any{
		"type": "DEPLOYS_FROM", "direction": "outgoing",
		"source_name": "edge-api", "source_id": "repo-edge",
		"target_name": "platform", "target_id": "repo-platform",
		"evidence_type": "argocd_application", "resolved_id": "res-1",
		"generation_id": "gen-9", "confidence": 0.9, "evidence_count": 3,
		"evidence_kinds":    []string{"argocd_application"},
		"resolution_source": "graph", "confidence_basis": "inferred",
		"rationale": "a long rationale that must not ship under handles",
	}
	relationshipOverview := map[string]any{
		"relationship_count": 2,
		"relationships": []map[string]any{
			relationship,
			{"type": "DEPENDS_ON", "target_name": "lib", "target_id": "repo-lib"},
		},
		"relationship_types": []string{"DEPLOYS_FROM", "DEPENDS_ON"},
		"evidence_types":     []string{"argocd_application"},
		"controller_driven":  []map[string]any{relationship},
		"story":              "Depends on lib.",
	}
	deploymentEvidence := map[string]any{
		"truth_basis":    "graph",
		"artifact_count": 1,
		"artifacts": []map[string]any{{
			"id": "art-1", "direction": "outgoing", "name": "api", "domain": "deploy",
			"path": "charts/api", "evidence_kind": "helm_release", "artifact_family": "helm",
			"extractor": "helm", "relationship_type": "DEPLOYS_FROM",
			"source_repo_id": "repo-edge", "source_repo_name": "edge-api",
			"source_repo_canonical_id": "canonical:edge", "source_repo_scope_key": "scope:s_12345678",
			"target_repo_id": "repo-platform", "target_repo_name": "platform",
			"evidence_source":       "graph",
			"resolved_id":           "res-1",
			"postgres_lookup_basis": "resolved_id",
			"commit_sha":            "abc123",
			"matched_value":         "some long matched manifest value",
			"source_location":       map[string]any{"repo_id": "repo-edge", "path": "charts/api"},
		}},
		"evidence_index": map[string]any{"by_family": map[string]any{"helm": 1}},
	}
	deploymentArtifacts := []map[string]any{{
		"relative_path": "Dockerfile", "artifact_type": "dockerfile",
		"artifact_name": "api-image", "service_name": "edge-api",
		"base_image": "golang:1.24", "cmd": "go build ./...",
		"shared_libraries": []string{"lib/a", "lib/b"},
		"pipeline_calls":   []string{"step one with a long description"},
		"entry_points":     []string{"main"},
		"env_files":        []string{".env"},
	}}
	deliveryPaths := []map[string]any{{
		"path": "charts/api", "kind": "controller_artifact", "controller_kind": "helm",
		"shared_libraries":       []string{"lib/a"},
		"ansible_playbook_hints": []map[string]any{{"playbook": "site.yml", "hint": "long hint body"}},
	}}
	deliveryWorkflows := []map[string]any{{
		"path": ".github/workflows/ci.yml", "controller_kind": "github_actions",
		"pipeline_calls": []string{"build", "test with a long description"},
		"shell_commands": []string{"make test"},
	}}
	return map[string]any{
		"repository":            map[string]any{"id": "repo-edge"},
		"subject":               map[string]any{"type": "repository", "id": "repo-edge"},
		"story":                 "Repository edge-api contains 3 indexed files.",
		"story_sections":        []map[string]any{{"title": "codebase", "summary": "3 files"}},
		"coverage_summary":      map[string]any{"status": "ok"},
		"limitations":           []string{},
		"truncated":             false,
		"drilldowns":            map[string]any{"context_path": "/api/v0/repositories/repo-edge/context"},
		"relationship_overview": relationshipOverview,
		"infrastructure_overview": map[string]any{
			"families":              []string{"helm"},
			"entity_family_counts":  map[string]int{"helm": 2},
			"relationship_overview": relationshipOverview,
			"deployment_evidence":   deploymentEvidence,
			"deployment_artifacts":  deploymentArtifacts,
		},
		"deployment_overview": map[string]any{
			"workload_count": 1, "workloads": []string{"edge-api"},
			"deployment_artifacts": deploymentArtifacts,
			"delivery_paths":       deliveryPaths,
			"delivery_workflows":   deliveryWorkflows,
			"delivery_family_paths": []map[string]any{{
				"family": "helm", "tool_family": "helm", "mode": "controller_delivery",
				"path": "charts/api", "production_evidence": true,
			}},
			"shared_config_paths":   []map[string]any{{"path": "shared/vars.yml", "source_repositories": []string{"a", "b"}}},
			"delivery_family_story": []string{"helm delivery via charts/api"},
			"topology_story":        []string{"edge-api deploys via charts/api"},
		},
		"ci_cd_evidence": map[string]any{
			"static_workflow_artifacts": map[string]any{"state": "present", "count": 2, "paths": []string{".github/workflows/ci.yml"}},
		},
		"documentation_overview": map[string]any{
			"repo_slug":           "acme/edge",
			"documentation_files": []string{"README.md"},
			"target_documentation": map[string]any{
				"findings": []map[string]any{{
					"finding_id": "finding:runbook", "finding_type": "runbook",
					"source_id": "source:docs", "document_id": "doc:runbook",
					"title":           "Runbook",
					"linked_entities": []any{map[string]any{"entity_type": "repository"}},
					"evidence":        map[string]any{"body": "a very long evidence body that must not ship under handles"},
				}},
				"finding_count": 1, "related_facts": []map[string]any{},
				"related_fact_count": 0,
				"coverage":           map[string]any{"target_fact_count": 1},
			},
		},
		"support_overview": map[string]any{
			"dependency_count": 1,
			"target_support": map[string]any{
				"evidence": []map[string]any{{
					"link_basis": "incident_routing.pagerduty_service", "fact_id": "fact:1",
					"fact_kind": "pagerduty_service", "scope_id": "scope:1",
					"payload": map[string]any{"summary": "a long payload body that must not ship under handles"},
				}},
				"evidence_count": 1, "ambiguous_evidence": []map[string]any{},
				"ambiguous_count": 0,
				"coverage":        map[string]any{"target_fact_count": 1},
			},
		},
		"framework_summary": map[string]any{
			"framework_count": 1,
			"frameworks": []map[string]any{{
				"framework": "go", "confidence": "high",
				"evidence_kinds": []string{"semantic_entity"}, "signal_count": 40,
				"paths": []string{"main.go", "handler.go"},
			}},
		},
		"semantic_overview": map[string]any{"entity_count": 40},
		"gitops_overview":   map[string]any{"enabled": true, "tool_families": []string{"argocd"}},
	}
}

// TestApplyRepoStoryEvidenceDetailFullIsIdentity proves full (and the empty
// default) changes nothing but the evidence_detail marker: no omission or
// drilldown appears and the body is otherwise unchanged.
func TestApplyRepoStoryEvidenceDetailFullIsIdentity(t *testing.T) {
	t.Parallel()

	for _, detail := range []string{"", ContextEvidenceDetailFull} {
		body := repoStoryDetailBody()
		before, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal before: %v", err)
		}
		if omissions := ApplyRepoStoryEvidenceDetail(body, detail); len(omissions) != 0 {
			t.Fatalf("detail %q omissions = %#v, want none", detail, omissions)
		}
		if got := StringVal(body, "evidence_detail"); got != ContextEvidenceDetailFull {
			t.Fatalf("detail %q evidence_detail = %q, want full", detail, got)
		}
		if _, has := body["evidence_detail_drilldown"]; has {
			t.Fatalf("detail %q set drilldown with nothing reduced", detail)
		}
		var beforeMap, afterMap map[string]any
		if err := json.Unmarshal(before, &beforeMap); err != nil {
			t.Fatalf("unmarshal before: %v", err)
		}
		after, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal after: %v", err)
		}
		if err := json.Unmarshal(after, &afterMap); err != nil {
			t.Fatalf("unmarshal after: %v", err)
		}
		delete(beforeMap, "evidence_detail")
		delete(afterMap, "evidence_detail")
		if !reflect.DeepEqual(beforeMap, afterMap) {
			t.Fatalf("detail %q mutated the body besides the marker", detail)
		}
	}
}

// TestApplyRepoStoryEvidenceDetailHandlesMarksEveryCut proves each bulky
// family ships projected rows with its omission total, duplicate partition
// lists collapse to ship-once pointers, counts stay untouched, and the
// drilldown names the full-rows path.
func TestApplyRepoStoryEvidenceDetailHandlesMarksEveryCut(t *testing.T) {
	t.Parallel()

	body := repoStoryDetailBody()
	omissions := ApplyRepoStoryEvidenceDetail(body, ContextEvidenceDetailHandles)

	if got := StringVal(body, "evidence_detail"); got != ContextEvidenceDetailHandles {
		t.Fatalf("evidence_detail = %q, want handles", got)
	}
	bySection := map[string]TruthOmission{}
	for _, omission := range omissions {
		bySection[omission.Section] = omission
	}

	rel := MapSliceValue(MapValue(body, "relationship_overview"), "relationships")
	if len(rel) != 2 {
		t.Fatalf("relationships = %d rows, want handles to keep the length", len(rel))
	}
	if _, has := rel[0]["rationale"]; has {
		t.Fatalf("relationship row = %#v, want identity keys only", rel[0])
	}
	if StringVal(rel[0], "resolved_id") != "res-1" {
		t.Fatalf("relationship row = %#v, want resolved_id kept as the drilldown key", rel[0])
	}
	overview := MapValue(body, "relationship_overview")
	if _, has := overview["controller_driven"]; has {
		t.Fatal("controller_driven still present; want rows shipped once under relationships")
	}
	if StringVal(overview, "controller_driven_shipped_at") != "relationships" {
		t.Fatalf("overview = %#v, want controller_driven_shipped_at pointer", overview)
	}
	if got := bySection["relationship_overview.relationships"].Total; got != 2 {
		t.Fatalf("relationships omission total = %d, want relationship rows held", got)
	}
	if bySection["relationship_overview.controller_driven"].Detail != contextEvidenceOmitted {
		t.Fatalf("partition omission = %#v, want detail omitted", bySection["relationship_overview.controller_driven"])
	}

	infra := MapValue(body, "infrastructure_overview")
	deployment := MapValue(infra, "deployment_evidence")
	artifacts := MapSliceValue(deployment, "artifacts")
	if len(artifacts) != 1 {
		t.Fatalf("deployment artifacts = %d rows, want 1", len(artifacts))
	}
	if _, has := artifacts[0]["source_location"]; has || artifacts[0]["commit_sha"] != nil {
		t.Fatalf("deployment artifact row = %#v, want identity keys only", artifacts[0])
	}
	if StringVal(artifacts[0], "resolved_id") != "res-1" {
		t.Fatalf("deployment artifact row = %#v, want resolved_id kept as the drilldown key", artifacts[0])
	}
	if _, has := deployment["evidence_index"]; has {
		t.Fatal("deployment evidence_index ships twice; want it folded into the artifacts omission")
	}
	if got := bySection["infrastructure_overview.deployment_evidence"].Total; got != 1 {
		t.Fatalf("deployment_evidence omission total = %d, want artifact_count 1", got)
	}

	deployOverview := MapValue(body, "deployment_overview")
	contentArtifacts := MapSliceValue(deployOverview, "deployment_artifacts")
	if len(contentArtifacts) != 1 {
		t.Fatalf("deployment_overview artifacts = %d rows, want 1", len(contentArtifacts))
	}
	if _, has := contentArtifacts[0]["pipeline_calls"]; has || contentArtifacts[0]["base_image"] != nil {
		t.Fatalf("content artifact row = %#v, want identity keys only", contentArtifacts[0])
	}
	paths := MapSliceValue(deployOverview, "delivery_paths")
	if _, has := paths[0]["ansible_playbook_hints"]; has || paths[0]["shared_libraries"] != nil {
		t.Fatalf("delivery path row = %#v, want identity keys only", paths[0])
	}
	workflows := MapSliceValue(deployOverview, "delivery_workflows")
	if _, has := workflows[0]["shell_commands"]; has || workflows[0]["pipeline_calls"] != nil {
		t.Fatalf("delivery workflow row = %#v, want identity keys only", workflows[0])
	}
	for section, want := range map[string]int{
		"deployment_overview.deployment_artifacts": 1,
		"deployment_overview.delivery_paths":       1,
		"deployment_overview.delivery_workflows":   1,
	} {
		if got, ok := bySection[section]; !ok || got.Detail != ContextEvidenceDetailHandles || got.Total != want {
			t.Fatalf("omission %q = %#v (present %v), want detail handles and total %d", section, got, ok, want)
		}
	}

	docs := MapValue(MapValue(body, "documentation_overview"), "target_documentation")
	findings := MapSliceValue(docs, "findings")
	if len(findings) != 1 {
		t.Fatalf("findings = %d rows, want 1", len(findings))
	}
	if _, has := findings[0]["evidence"]; has {
		t.Fatalf("finding row = %#v, want identity keys only", findings[0])
	}
	if StringVal(findings[0], "finding_id") != "finding:runbook" {
		t.Fatalf("finding row = %#v, want finding_id kept", findings[0])
	}
	if got := bySection["documentation_overview.target_documentation.findings"].Total; got != 1 {
		t.Fatalf("findings omission total = %d, want finding_count 1", got)
	}

	support := MapValue(MapValue(body, "support_overview"), "target_support")
	evidence := MapSliceValue(support, "evidence")
	if len(evidence) != 1 {
		t.Fatalf("support evidence = %d rows, want 1", len(evidence))
	}
	if _, has := evidence[0]["payload"]; has {
		t.Fatalf("support row = %#v, want identity keys only", evidence[0])
	}
	if got := bySection["support_overview.target_support.evidence"].Total; got != 1 {
		t.Fatalf("support omission total = %d, want evidence_count 1", got)
	}

	frameworks := MapSliceValue(MapValue(body, "framework_summary"), "frameworks")
	if _, has := frameworks[0]["paths"]; has {
		t.Fatalf("framework row = %#v, want identity keys only", frameworks[0])
	}
	if got := bySection["framework_summary.frameworks"].Total; got != 1 {
		t.Fatalf("frameworks omission total = %d, want framework_count 1", got)
	}

	// Counts, narrative, limitations, and scalar overviews ship untouched.
	if IntVal(MapValue(body, "relationship_overview"), "relationship_count") != 2 {
		t.Fatal("relationship_count changed; counts must reflect the full rows")
	}
	if IntVal(deployOverview, "workload_count") != 1 {
		t.Fatal("workload_count changed; counts must reflect the full rows")
	}
	if got := body["story"]; got != "Repository edge-api contains 3 indexed files." {
		t.Fatalf("story changed: %#v", got)
	}
	ciCD := MapValue(body, "ci_cd_evidence")
	if MapValue(ciCD, "static_workflow_artifacts")["paths"] == nil {
		t.Fatal("ci_cd static workflow paths dropped; workflow-file identity ships under handles")
	}
	if omission, has := bySection["ci_cd_evidence"]; has {
		t.Fatalf("ci_cd omission = %#v, want none: the family keeps its counts and paths", omission)
	}

	drilldown := MapValue(body, "evidence_detail_drilldown")
	if StringVal(drilldown, "artifact_key") != "resolved_id" {
		t.Fatalf("evidence_detail_drilldown = %#v, want artifact_key resolved_id", drilldown)
	}
}

// TestApplyRepoStoryEvidenceDetailTolerantOfSparseAndDegraded proves a story
// with none of the bulky families neither panics nor reports an omission, and
// that shaping never clears limitations or truncated.
func TestApplyRepoStoryEvidenceDetailTolerantOfSparseAndDegraded(t *testing.T) {
	t.Parallel()

	sparse := map[string]any{
		"repository": map[string]any{"id": "repo-new"},
		"story":      "Repository new contains 0 indexed files.",
	}
	if omissions := ApplyRepoStoryEvidenceDetail(sparse, ContextEvidenceDetailHandles); len(omissions) != 0 {
		t.Fatalf("sparse omissions = %#v, want none", omissions)
	}
	if _, has := sparse["evidence_detail_drilldown"]; has {
		t.Fatal("evidence_detail_drilldown set with nothing reduced")
	}
	if got := StringVal(sparse, "evidence_detail"); got != ContextEvidenceDetailHandles {
		t.Fatalf("evidence_detail = %q, want handles", got)
	}

	degraded := map[string]any{
		"repository":  map[string]any{"id": "repo-degraded"},
		"limitations": []string{"infrastructure_read_degraded"},
		"truncated":   true,
	}
	if omissions := ApplyRepoStoryEvidenceDetail(degraded, ContextEvidenceDetailHandles); len(omissions) != 0 {
		t.Fatalf("degraded omissions = %#v, want none", omissions)
	}
	limitations, _ := degraded["limitations"].([]string)
	if len(limitations) != 1 || limitations[0] != "infrastructure_read_degraded" {
		t.Fatalf("limitations = %#v, want degradation signals kept", degraded["limitations"])
	}
	if degraded["truncated"] != true {
		t.Fatalf("truncated = %#v, want true kept", degraded["truncated"])
	}
}

// TestApplyRepoStoryEvidenceDetailDrilldownOmitsArtifactHintWithoutResolvedIDRows
// proves the machine-actionable artifact hint ships only when a reduced family
// carries resolved_id (relationships, deployment evidence, #10605). A
// findings-only cut keeps the full-rows repeat as the stated path instead of
// an artifact_key no reduced row carries.
func TestApplyRepoStoryEvidenceDetailDrilldownOmitsArtifactHintWithoutResolvedIDRows(t *testing.T) {
	t.Parallel()

	body := map[string]any{
		"repository": map[string]any{"id": "repo-edge"},
		"documentation_overview": map[string]any{
			"target_documentation": map[string]any{
				"findings": []map[string]any{{
					"finding_id": "finding:runbook", "finding_type": "runbook",
					"source_id": "source:docs", "document_id": "doc:runbook",
					"title":           "Runbook",
					"linked_entities": []any{map[string]any{"entity_type": "repository"}},
					"evidence":        map[string]any{"body": "a long body that must not ship under handles"},
				}},
				"finding_count": 1,
			},
		},
	}
	omissions := ApplyRepoStoryEvidenceDetail(body, ContextEvidenceDetailHandles)
	if len(omissions) == 0 {
		t.Fatal("omissions empty, want the findings cut recorded")
	}
	drilldown := MapValue(body, "evidence_detail_drilldown")
	if StringVal(drilldown, "full_rows") == "" {
		t.Fatalf("evidence_detail_drilldown = %#v, want the full-rows repeat path", drilldown)
	}
	if _, has := drilldown["artifact_tool"]; has {
		t.Fatalf("evidence_detail_drilldown = %#v, want no artifact_tool without resolved_id rows", drilldown)
	}
	if _, has := drilldown["artifact_key"]; has {
		t.Fatalf("evidence_detail_drilldown = %#v, want no artifact_key without resolved_id rows", drilldown)
	}
}

// TestApplyRepoStoryEvidenceDetailNeverMutatesSharedRows proves the shaper
// builds new maps: bodies assembled from shared read-model rows, including
// the relationship overview shared between the top level and the
// infrastructure overview, keep every field.
func TestApplyRepoStoryEvidenceDetailNeverMutatesSharedRows(t *testing.T) {
	t.Parallel()

	body := repoStoryDetailBody()
	relationship := MapSliceValue(MapValue(body, "relationship_overview"), "relationships")[0]
	artifact := MapSliceValue(MapValue(MapValue(body, "infrastructure_overview"), "deployment_evidence"), "artifacts")[0]
	finding := MapSliceValue(MapValue(MapValue(body, "documentation_overview"), "target_documentation"), "findings")[0]
	infraRelationships := MapSliceValue(MapValue(MapValue(body, "infrastructure_overview"), "relationship_overview"), "relationships")

	ApplyRepoStoryEvidenceDetail(body, ContextEvidenceDetailHandles)

	if _, has := relationship["rationale"]; !has {
		t.Fatal("relationship row lost rationale; the shaper mutated a shared row")
	}
	if _, has := artifact["source_location"]; !has {
		t.Fatal("deployment artifact lost source_location; the shaper mutated a shared row")
	}
	if _, has := finding["evidence"]; !has {
		t.Fatal("finding lost evidence; the shaper mutated a shared row")
	}
	if len(infraRelationships) != 2 {
		t.Fatal("infrastructure relationship rows were cut in place; the shaper mutated a shared overview")
	}
}
