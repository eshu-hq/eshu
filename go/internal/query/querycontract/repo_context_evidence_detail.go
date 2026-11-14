// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "sort"

// repoContextDependencyHandleKeys keeps a top-level relationships row (the
// outgoing dependency list) to the same identity keys.
var repoContextDependencyHandleKeys = []string{
	"type", "target_name", "target_id",
	"evidence_type", "resolved_id", "generation_id", "confidence",
	"evidence_count", "evidence_kinds", "resolution_source", "confidence_basis",
}

// repoContextConsumerHandleKeys keeps a consumer row to its identity. Consumer
// rows are already identity-shaped ({name, id}); the projection keeps the
// list length stable while naming the family in omissions when rows exist.
var repoContextConsumerHandleKeys = []string{"name", "id"}

// repoContextEntryPointHandleKeys keeps an entry-point row to its identity.
// The extraction reason ships only under full.
var repoContextEntryPointHandleKeys = []string{"name", "relative_path", "language"}

// repoContextInfraHandleKeys keeps an infrastructure row to the identity
// its builders emit (type|name|file_path per
// repositoryInfrastructureEntryKey) plus kind, the real optional
// classifier. Per-row evidence detail ships only under full.
var repoContextInfraHandleKeys = []string{
	"type", "name", "file_path", "kind",
}

// RepoContextHandleListFamilies returns the repository-context row families
// the handles view projects, in projection order. The OpenAPI
// evidence_detail text and the MCP get_repo_context tool text name these
// families; their tests range over this list so a family added here cannot
// ship undescribed.
func RepoContextHandleListFamilies() []string {
	return []string{
		"relationships",
		"relationship_overview.relationships",
		"consumers",
		"api_surface.endpoints",
		"deployment_evidence.artifacts",
		"deployment_artifacts",
		"infrastructure_overview.deployment_artifacts",
		"entry_points",
		"infrastructure",
	}
}

// ApplyRepoContextEvidenceDetail shapes a repository-context payload in place
// after the dossier is built, so every count is computed from the full rows
// and the shaping changes only what ships. An empty detail means full. It
// sets evidence_detail only under handles; full leaves the body untouched,
// byte-identical to the pre-change shape.
//
// Under handles it projects the bulky row families to identity rows, drops
// the repeated relationship partition lists and the evidence_index regrouping
// with a pointer, keeps every count, and returns one truth omission per
// reduced family with the rows it held. Scalar counts, coverage, and partial
// reasons ship untouched.
//
// Rows are projected into new maps, so a map shared with a read model is
// never mutated. Call ValidateContextEvidenceDetail first.
func ApplyRepoContextEvidenceDetail(data map[string]any, detail string) []TruthOmission {
	if detail == "" {
		detail = ContextEvidenceDetailFull
	}
	if detail != ContextEvidenceDetailHandles {
		return nil
	}
	data["evidence_detail"] = detail
	var omissions []TruthOmission
	resolvedHint := false
	reduce := func(projected []TruthOmission, carriesResolvedID bool) {
		if carriesResolvedID && len(projected) > 0 {
			resolvedHint = true
		}
		omissions = append(omissions, projected...)
	}
	reduce(projectRepoContextTopRelationships(data), true)
	if len(MapValue(data, "relationship_overview")) > 0 {
		reduce(projectRepoStoryRelationships(data), true)
	}
	reduce(projectRepoContextConsumers(data), false)
	if len(MapValue(data, "api_surface")) > 0 {
		if omission, ok := projectRepoContextEndpointsIn(data); ok {
			reduce([]TruthOmission{omission}, false)
		}
	}
	reduce(projectRepoContextDeploymentEvidence(data), true)
	reduce(projectRepoContextTopArtifacts(data, "deployment_artifacts", "deployment_artifacts"), false)
	if infra := MapValue(data, "infrastructure_overview"); len(infra) > 0 {
		reduce(projectRepoContextTopArtifacts(infra, "deployment_artifacts", "infrastructure_overview.deployment_artifacts"), false)
	}
	reduce(projectRepoContextTopLists(data), false)
	if len(omissions) == 0 {
		return nil
	}
	drilldown := map[string]any{
		"full_rows": "repeat the call with evidence_detail full for every field of every row; " +
			"under handles each list keeps its length and drops per-row detail",
	}
	if resolvedHint {
		drilldown["artifact_tool"] = "get_relationship_evidence"
		drilldown["artifact_key"] = "resolved_id"
	}
	data["evidence_detail_drilldown"] = drilldown
	return omissions
}

// projectRepoContextTopRelationships projects the top-level relationships
// list (outgoing dependencies) to identity rows.
func projectRepoContextTopRelationships(data map[string]any) []TruthOmission {
	rows := MapSliceValue(data, "relationships")
	if len(rows) == 0 {
		return nil
	}
	data["relationships"] = projectContextRows(rows, repoContextDependencyHandleKeys)
	return []TruthOmission{{
		Section: "relationships",
		Detail:  ContextEvidenceDetailHandles,
		Total:   len(rows),
	}}
}

// projectRepoContextConsumers projects the consumers list to identity rows.
// Consumer rows are already {name, id}; the projection keeps them byte-stable
// while still naming the family in omissions when rows exist.
func projectRepoContextConsumers(data map[string]any) []TruthOmission {
	rows := MapSliceValue(data, "consumers")
	if len(rows) == 0 {
		return nil
	}
	data["consumers"] = projectContextRows(rows, repoContextConsumerHandleKeys)
	return []TruthOmission{{
		Section: "consumers",
		Detail:  ContextEvidenceDetailHandles,
		Total:   len(rows),
	}}
}

// projectRepoContextEndpointsIn projects api_surface.endpoints to identity
// rows. The endpoint count stays untouched.
func projectRepoContextEndpointsIn(data map[string]any) (TruthOmission, bool) {
	surface := MapValue(data, "api_surface")
	endpoints := MapSliceValue(surface, "endpoints")
	if len(endpoints) == 0 {
		return TruthOmission{}, false
	}
	shaped := CopyMap(surface)
	shaped["endpoints"] = projectContextRows(endpoints, contextEndpointHandleKeys)
	data["api_surface"] = shaped
	return TruthOmission{
		Section: "api_surface.endpoints",
		Detail:  ContextEvidenceDetailHandles,
		Total:   max(len(endpoints), IntVal(surface, "endpoint_count")),
	}, true
}

// projectRepoContextDeploymentEvidence projects the graph deployment-evidence
// artifacts to identity rows and folds the evidence_index regrouping into the
// same omission.
func projectRepoContextDeploymentEvidence(data map[string]any) []TruthOmission {
	evidence := MapValue(data, "deployment_evidence")
	artifacts := MapSliceValue(evidence, "artifacts")
	if len(artifacts) == 0 {
		return nil
	}
	shaped := CopyMap(evidence)
	shaped["artifacts"] = projectContextRows(artifacts, repoStoryDeploymentEvidenceHandleKeys)
	delete(shaped, "evidence_index")
	data["deployment_evidence"] = shaped
	return []TruthOmission{{
		Section: "deployment_evidence.artifacts",
		Detail:  ContextEvidenceDetailHandles,
		Total:   max(len(artifacts), IntVal(evidence, "artifact_count")),
	}}
}

// projectRepoContextTopArtifacts projects a deployment_artifacts map of
// named lists held either at the top level or inside an overview holder.
// Production stores one named list per artifact source, merged by
// MergeDeploymentArtifactMaps, so a flat-list read would no-op on real data.
// Every named list is projected to identity rows (names sorted for a stable
// omission order) and the family carries one omission with the summed total,
// keeping the RepoContextHandleListFamilies naming.
func projectRepoContextTopArtifacts(holder map[string]any, key, section string) []TruthOmission {
	artifacts := MapValue(holder, key)
	if len(artifacts) == 0 {
		return nil
	}
	names := make([]string, 0, len(artifacts))
	for name := range artifacts {
		names = append(names, name)
	}
	sort.Strings(names)
	// Copy before projecting: the top-level and overview holders can alias
	// the same map, and a map shared with a read model is never mutated.
	shaped := CopyMap(artifacts)
	total := 0
	for _, name := range names {
		rows := MapSliceValue(artifacts, name)
		if len(rows) == 0 {
			continue
		}
		shaped[name] = projectContextRows(rows, repoStoryDeploymentArtifactHandleKeys)
		total += len(rows)
	}
	if total == 0 {
		return nil
	}
	holder[key] = shaped
	return []TruthOmission{{
		Section: section,
		Detail:  ContextEvidenceDetailHandles,
		Total:   total,
	}}
}

// projectRepoContextTopLists projects entry_points and infrastructure to
// identity rows.
func projectRepoContextTopLists(data map[string]any) []TruthOmission {
	var omissions []TruthOmission
	if rows := MapSliceValue(data, "entry_points"); len(rows) > 0 {
		data["entry_points"] = projectContextRows(rows, repoContextEntryPointHandleKeys)
		omissions = append(omissions, TruthOmission{
			Section: "entry_points",
			Detail:  ContextEvidenceDetailHandles,
			Total:   len(rows),
		})
	}
	if rows := MapSliceValue(data, "infrastructure"); len(rows) > 0 {
		data["infrastructure"] = projectContextRows(rows, repoContextInfraHandleKeys)
		omissions = append(omissions, TruthOmission{
			Section: "infrastructure",
			Detail:  ContextEvidenceDetailHandles,
			Total:   len(rows),
		})
	}
	return omissions
}
