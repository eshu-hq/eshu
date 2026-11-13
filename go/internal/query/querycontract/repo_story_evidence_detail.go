// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "fmt"

// repoStoryRelationshipHandleKeys keep a repository relationship row to the
// identity and truth-label fields a caller needs to name the edge and drill
// into it: the endpoints, the relationship and evidence types, and the
// resolved_id get_relationship_evidence takes, plus how the edge was
// established (generation, confidence, counts, kinds, basis). The rationale
// prose ships only under full.
var repoStoryRelationshipHandleKeys = []string{
	"type", "direction", "source_name", "source_id", "target_name", "target_id",
	"evidence_type", "resolved_id", "generation_id", "confidence",
	"evidence_count", "evidence_kinds", "resolution_source", "confidence_basis",
}

// repoStoryRelationshipPartitions are the relationship_overview row lists that
// repeat the relationships rows partitioned by evidence family. Under handles
// they ship once, under relationships, and each partition keeps a pointer.
var repoStoryRelationshipPartitions = []string{
	"controller_driven", "workflow_driven", "iac_driven", "other_relationships",
}

// repoStoryDeploymentEvidenceHandleKeys keep a graph deployment-evidence
// artifact row to its identity plus how the edge was established: the anchor
// endpoints, the evidence and relationship types, the drilldown resolved_id,
// and the short classification scalars. Observation identity
// (canonical/scope keys), commit SHAs, matched values, line numbers, and the
// nested source_location ship only under full; they are derivable from the
// kept endpoint and path keys.
var repoStoryDeploymentEvidenceHandleKeys = []string{
	"id", "direction", "name", "domain", "path", "evidence_kind", "artifact_family",
	"extractor", "relationship_type", "source_repo_id", "source_repo_name",
	"target_repo_id", "target_repo_name", "evidence_source",
	"resolved_id", "postgres_lookup_basis", "generation_id", "confidence",
	"environment", "runtime_platform_kind", "matched_alias", "ref_value", "ref_pinned",
}

// repoStoryDeploymentArtifactHandleKeys keep a content-derived deployment
// artifact row to the path, kind, and name fields that identify it. The
// evidence bodies (shared libraries, pipeline calls, entry points, env
// files, ansible detail, base images, build commands) ship only under full.
var repoStoryDeploymentArtifactHandleKeys = []string{
	"path", "relative_path", "controller_kind", "artifact_type", "artifact_name", "service_name",
}

// repoStoryDeliveryPathHandleKeys keep a delivery path row to the path, kind,
// and family fields that identify it. The row merges the artifact-derived
// delivery view with the graph evidence delivery view, so the keys cover both
// spellings; the per-row call and hint lists ship only under full.
var repoStoryDeliveryPathHandleKeys = []string{
	"path", "kind", "type", "controller_kind", "artifact_type", "artifact_name",
	"service_name", "family", "tool_family", "mode", "target_name", "target_id",
	"evidence_type", "production_evidence",
}

// repoStoryDeliveryWorkflowHandleKeys keep a delivery workflow row to its
// path and controller kind. The call, command, and ansible lists ship only
// under full.
var repoStoryDeliveryWorkflowHandleKeys = []string{"path", "controller_kind"}

// repoStoryFindingHandleKeys keep a target-documentation finding row to its
// identity: the finding, its type and source document, its title, and the
// linked entities that admitted it to the target. The evidence body ships
// only under full.
var repoStoryFindingHandleKeys = []string{
	"finding_id", "finding_type", "source_id", "document_id", "title", "linked_entities",
}

// repoStoryRelatedFactHandleKeys keep a documentation related-fact row to its
// identity. The payload ships only under full.
var repoStoryRelatedFactHandleKeys = []string{
	"fact_id", "fact_kind", "scope_id", "generation_id", "source_system", "observed_at",
}

// repoStorySupportEvidenceHandleKeys keep a target-support evidence row to
// its identity and link basis. The payload ships only under full.
var repoStorySupportEvidenceHandleKeys = []string{
	"link_basis", "fact_id", "fact_kind", "scope_id", "generation_id",
	"source_system", "observed_at", "source_record_id", "linked_via_fact_id",
}

// repoStoryFrameworkHandleKeys keep a framework summary row to the framework,
// its confidence, and its signal counts. The per-file path list ships only
// under full.
var repoStoryFrameworkHandleKeys = []string{
	"framework", "confidence", "evidence_kinds", "signal_count",
}

// RepoStoryHandleListFamilies returns the repository-story row families the
// handles view projects, in projection order. The OpenAPI evidence_detail
// text and the MCP get_repo_story tool text name these families; their tests
// range over this list so a family added here cannot ship undescribed.
func RepoStoryHandleListFamilies() []string {
	return []string{
		"relationship_overview.relationships",
		"deployment_evidence.artifacts",
		"deployment_artifacts",
		"delivery_paths",
		"delivery_workflows",
		"target_documentation.findings",
		"target_support.evidence",
		"framework_summary.frameworks",
	}
}

// ApplyRepoStoryEvidenceDetail shapes a repository-story payload in place
// after the dossier is built, so every count, status, limitation, and
// narrative field is computed from the full rows and the shaping changes
// only what ships. An empty detail means full. It always sets
// evidence_detail.
//
// Under handles it projects the bulky row families to identity rows: the
// relationship rows (both the top-level relationship_overview and the copy
// nested in infrastructure_overview), the graph deployment-evidence artifacts
// (folding the evidence_index regrouping into the same omission), the
// content-derived deployment artifacts in both overview locations, the
// delivery path and workflow rows, the target-documentation findings and
// related facts, the target-support evidence rows, and the framework rows.
// Every projected list keeps its length, so a count never disagrees with its
// list. Partition lists that repeat the relationship rows are dropped with a
// <list>_shipped_at pointer. Scalar overviews, counts, limitations, the
// narrative, the CI/CD workflow-file identity, and the file-derived
// documentation path lists ship untouched.
//
// It returns one truth omission per reduced list with the rows it held, and
// sets evidence_detail_drilldown to say how to fetch the full rows. Rows are
// projected into new maps, so a map shared with a read model — including the
// relationship overview the builder attaches in two places — is never
// mutated. Call ValidateContextEvidenceDetail first.
func ApplyRepoStoryEvidenceDetail(data map[string]any, detail string) []TruthOmission {
	if detail == "" {
		detail = ContextEvidenceDetailFull
	}
	data["evidence_detail"] = detail
	if detail != ContextEvidenceDetailHandles {
		return nil
	}
	var omissions []TruthOmission
	omissions = append(omissions, projectRepoStoryRelationships(data)...)
	if infra := MapValue(data, "infrastructure_overview"); len(infra) > 0 {
		omissions = append(omissions, projectRepoStoryRelationshipsIn(infra, "infrastructure_overview")...)
		omissions = append(omissions, projectRepoStoryDeploymentEvidence(infra, "infrastructure_overview")...)
		omissions = append(omissions, projectRepoStoryContentArtifacts(infra, "infrastructure_overview")...)
	}
	if overview := MapValue(data, "deployment_overview"); len(overview) > 0 {
		omissions = append(omissions, projectRepoStoryContentArtifacts(overview, "deployment_overview")...)
		omissions = append(omissions, projectRepoStoryDeliveryLists(overview)...)
	}
	omissions = append(omissions, projectRepoStoryTargetDocumentation(data)...)
	omissions = append(omissions, projectRepoStoryTargetSupport(data)...)
	omissions = append(omissions, projectRepoStoryFrameworks(data)...)
	if len(omissions) == 0 {
		return nil
	}
	data["evidence_detail_drilldown"] = map[string]any{
		"full_rows": fmt.Sprintf(
			"repeat the call with evidence_detail full for every field of every row; " +
				"under handles each list keeps its length and drops per-row detail; " +
				"narrow first via the repository context, stats, and coverage paths in drilldowns"),
		"artifact_tool": "get_relationship_evidence",
		"artifact_key":  "resolved_id",
	}
	return omissions
}

// projectRepoStoryRelationships projects the relationship_overview held in
// data (the top-level one) to identity rows and collapses its partition
// lists to ship-once pointers.
func projectRepoStoryRelationships(data map[string]any) []TruthOmission {
	return projectRepoStoryRelationshipsIn(data, "")
}

// projectRepoStoryRelationshipsIn projects holder's relationship_overview.
// The section prefix names the holder (empty for the top-level overview) so
// the omission says which copy was reduced.
func projectRepoStoryRelationshipsIn(holder map[string]any, prefix string) []TruthOmission {
	overview := MapValue(holder, "relationship_overview")
	if len(overview) == 0 {
		return nil
	}
	section := "relationship_overview"
	if prefix != "" {
		section = prefix + ".relationship_overview"
	}
	var omissions []TruthOmission
	rows := MapSliceValue(overview, "relationships")
	if len(rows) > 0 {
		shaped := CopyMap(overview)
		shaped["relationships"] = projectContextRows(rows, repoStoryRelationshipHandleKeys)
		for _, partition := range repoStoryRelationshipPartitions {
			if len(MapSliceValue(shaped, partition)) == 0 {
				continue
			}
			delete(shaped, partition)
			shaped[partition+"_shipped_at"] = "relationships"
			omissions = append(omissions, TruthOmission{
				Section: section + "." + partition,
				Detail:  contextEvidenceOmitted,
				Total:   len(MapSliceValue(overview, partition)),
			})
		}
		holder["relationship_overview"] = shaped
		omissions = append(omissions, TruthOmission{
			Section: section + ".relationships",
			Detail:  ContextEvidenceDetailHandles,
			Total:   max(len(rows), IntVal(overview, "relationship_count")),
		})
		return omissions
	}
	// No relationship rows: partitions repeat nothing, so they stay as the
	// builder emitted them and no cut is marked.
	return nil
}

// projectRepoStoryDeploymentEvidence projects the graph deployment-evidence
// artifacts in holder (the infrastructure overview) to identity rows and
// folds the evidence_index regrouping into the same omission.
func projectRepoStoryDeploymentEvidence(holder map[string]any, prefix string) []TruthOmission {
	evidence := MapValue(holder, "deployment_evidence")
	artifacts := MapSliceValue(evidence, "artifacts")
	if len(artifacts) == 0 {
		return nil
	}
	shaped := CopyMap(evidence)
	shaped["artifacts"] = projectContextRows(artifacts, repoStoryDeploymentEvidenceHandleKeys)
	delete(shaped, "evidence_index")
	holder["deployment_evidence"] = shaped
	return []TruthOmission{{
		Section: prefix + ".deployment_evidence",
		Detail:  ContextEvidenceDetailHandles,
		Total:   max(len(artifacts), IntVal(evidence, "artifact_count")),
	}}
}

// projectRepoStoryContentArtifacts projects the content-derived deployment
// artifacts in holder (either overview location) to identity rows.
func projectRepoStoryContentArtifacts(holder map[string]any, prefix string) []TruthOmission {
	rows := MapSliceValue(holder, "deployment_artifacts")
	if len(rows) == 0 {
		return nil
	}
	holder["deployment_artifacts"] = projectContextRows(rows, repoStoryDeploymentArtifactHandleKeys)
	return []TruthOmission{{
		Section: prefix + ".deployment_artifacts",
		Detail:  ContextEvidenceDetailHandles,
		Total:   len(rows),
	}}
}

// projectRepoStoryDeliveryLists projects the delivery path and workflow rows
// of the deployment overview to identity rows. The family-path,
// shared-config, and story string lists are already identity or narrative
// summaries and ship whole.
func projectRepoStoryDeliveryLists(overview map[string]any) []TruthOmission {
	var omissions []TruthOmission
	if rows := MapSliceValue(overview, "delivery_paths"); len(rows) > 0 {
		overview["delivery_paths"] = projectContextRows(rows, repoStoryDeliveryPathHandleKeys)
		omissions = append(omissions, TruthOmission{
			Section: "deployment_overview.delivery_paths",
			Detail:  ContextEvidenceDetailHandles,
			Total:   len(rows),
		})
	}
	if rows := MapSliceValue(overview, "delivery_workflows"); len(rows) > 0 {
		overview["delivery_workflows"] = projectContextRows(rows, repoStoryDeliveryWorkflowHandleKeys)
		omissions = append(omissions, TruthOmission{
			Section: "deployment_overview.delivery_workflows",
			Detail:  ContextEvidenceDetailHandles,
			Total:   len(rows),
		})
	}
	return omissions
}

// projectRepoStoryTargetDocumentation projects the target-documentation
// findings and related facts to identity rows. The file-derived path lists
// and every count ship untouched.
func projectRepoStoryTargetDocumentation(data map[string]any) []TruthOmission {
	target := MapValue(MapValue(data, "documentation_overview"), "target_documentation")
	if len(target) == 0 {
		return nil
	}
	var omissions []TruthOmission
	if rows := MapSliceValue(target, "findings"); len(rows) > 0 {
		shaped := CopyMap(target)
		shaped["findings"] = projectContextRows(rows, repoStoryFindingHandleKeys)
		setRepoStoryNested(data, "documentation_overview", "target_documentation", shaped)
		omissions = append(omissions, TruthOmission{
			Section: "documentation_overview.target_documentation.findings",
			Detail:  ContextEvidenceDetailHandles,
			Total:   max(len(rows), IntVal(target, "finding_count")),
		})
		target = shaped
	}
	if rows := MapSliceValue(target, "related_facts"); len(rows) > 0 {
		shaped := CopyMap(target)
		shaped["related_facts"] = projectContextRows(rows, repoStoryRelatedFactHandleKeys)
		setRepoStoryNested(data, "documentation_overview", "target_documentation", shaped)
		omissions = append(omissions, TruthOmission{
			Section: "documentation_overview.target_documentation.related_facts",
			Detail:  ContextEvidenceDetailHandles,
			Total:   max(len(rows), IntVal(target, "related_fact_count")),
		})
	}
	return omissions
}

// projectRepoStoryTargetSupport projects the target-support evidence rows to
// identity rows, dropping the per-row payloads. Every family count, the
// coverage, and the missing evidence ship untouched.
func projectRepoStoryTargetSupport(data map[string]any) []TruthOmission {
	target := MapValue(MapValue(data, "support_overview"), "target_support")
	if len(target) == 0 {
		return nil
	}
	var omissions []TruthOmission
	for _, list := range []struct {
		key      string
		countKey string
		section  string
	}{
		{"evidence", "evidence_count", "support_overview.target_support.evidence"},
		{"ambiguous_evidence", "ambiguous_count", "support_overview.target_support.ambiguous_evidence"},
	} {
		rows := MapSliceValue(target, list.key)
		if len(rows) == 0 {
			continue
		}
		shaped := CopyMap(target)
		shaped[list.key] = projectContextRows(rows, repoStorySupportEvidenceHandleKeys)
		setRepoStoryNested(data, "support_overview", "target_support", shaped)
		target = shaped
		omissions = append(omissions, TruthOmission{
			Section: list.section,
			Detail:  ContextEvidenceDetailHandles,
			Total:   max(len(rows), IntVal(target, list.countKey)),
		})
	}
	return omissions
}

// projectRepoStoryFrameworks projects the framework summary rows to identity
// rows, dropping the per-framework file path lists. The framework count and
// story ship untouched.
func projectRepoStoryFrameworks(data map[string]any) []TruthOmission {
	summary := MapValue(data, "framework_summary")
	rows := MapSliceValue(summary, "frameworks")
	if len(rows) == 0 {
		return nil
	}
	shaped := CopyMap(summary)
	shaped["frameworks"] = projectContextRows(rows, repoStoryFrameworkHandleKeys)
	data["framework_summary"] = shaped
	return []TruthOmission{{
		Section: "framework_summary.frameworks",
		Detail:  ContextEvidenceDetailHandles,
		Total:   max(len(rows), IntVal(summary, "framework_count")),
	}}
}

// setRepoStoryNested replaces data[outer][inner] with shaped, copying the
// outer map so a shared overview is never mutated.
func setRepoStoryNested(data map[string]any, outer, inner string, shaped map[string]any) {
	holder := CopyMap(MapValue(data, outer))
	holder[inner] = shaped
	data[outer] = holder
}
