// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "fmt"

// storyListHandleKeys names the top-level story lists the handles view
// projects and the identity keys each row keeps. The rows come from the same
// builders the full view uses, so a handle row is the full row minus the keys
// not listed here; every list keeps its length.
var storyListHandleKeys = []struct {
	key  string
	keys []string
}{
	{"upstream_dependencies", []string{"resolved_id", "relationship_type", "source", "source_repo_id", "target", "target_repo_id", "target_id", "confidence"}},
	{"dependents", []string{"repo_id", "repository"}},
	{"consumer_repositories", []string{"repo_id", "repository"}},
	{"provisioning_source_chains", []string{"repo_id", "repository", "confidence"}},
	{"hostnames", []string{"hostname", "environment", "visibility"}},
	{"entrypoints", []string{"type", "target", "hostname", "environment", "visibility"}},
	{"cloud_resources", []string{"id", "name", "kind", "resource_type", "confidence", "relationship_basis"}},
	{"uncorrelated_cloud_resources", storyCloudCandidateHandleKeys},
}

// storyCloudCandidateHandleKeys keep an uncorrelated cloud resource candidate
// to its identity plus every label that says why it is not promoted:
// candidate_status, missing_relationship, match_basis, and the service anchor
// status and reason. Dropping them would turn "not correlated, and why" into a
// bare resource name.
var storyCloudCandidateHandleKeys = []string{
	"id", "name", "kind", "resource_type",
	"candidate_status", "missing_relationship", "match_basis",
	"service_anchor_status", "service_anchor_reason", "relationship_basis",
}

// storyDownstreamShippedAt maps each downstream_consumers row list to the
// top-level story list that already ships the same rows. Under handles the
// rows ship once, there, and downstream_consumers keeps its counts and a
// pointer (the same shape deployment_overview.api_surface uses).
var storyDownstreamShippedAt = []struct{ rowsKey, countKey, shippedAt string }{
	{"graph_dependents", "graph_dependent_count", "dependents"},
	{"content_consumers", "content_consumer_count", "consumer_repositories"},
}

// storyTracePreviewLimit is how many evidence handles a trace segment keeps
// when its rows also ship in a top-level section of the story. evidence_count
// stays the true total.
const storyTracePreviewLimit = 10

// storyTraceDuplicatedSegments are the code_to_runtime_trace segments whose
// evidence rows repeat a top-level story section: code_entrypoints repeats
// api_surface.endpoints and entrypoints, deployment_config repeats
// deployment_evidence, and cloud_dependencies repeats cloud_resources. The
// other segments are the only copy of their rows, so they keep every handle.
var storyTraceDuplicatedSegments = map[string]bool{
	"code_entrypoints":   true,
	"deployment_config":  true,
	"cloud_dependencies": true,
}

// storyTraceHandleKeys are the identity and truth-label keys each
// code_to_runtime_trace segment keeps per evidence row. A segment that is the
// only copy of its rows (service_identity, ci_cd, image_package, runtime) must
// keep enough to identify each row, and the image_package segment is the only
// place the story carries the image digest and SBOM identity. A name not listed
// falls back to storyTraceFallbackHandleKeys.
var storyTraceHandleKeys = map[string][]string{
	"service_identity": {"service_id", "service_name", "repo_id", "repo_name"},
	"code_entrypoints": {"id", "type", "path", "methods", "target", "environment", "visibility"},
	"ci_cd":            {"path", "tool_family", "kind", "environment", storyEvidenceSummaryShippedAt},
	"image_package": {
		"image_ref", "digest", "repository_id", "identity_id", "identity_outcome", "identity_strength",
		"sbom_attachment_id", "sbom_document_id", "sbom_document_digest", "sbom_attachment_status",
		"sbom_artifact_kind", "sbom_format", "package_ref", "path", "tool_family", "source_key",
	},
	"deployment_config": {"id", "resolved_id", "relationship_type", "confidence", "path", "tool_family", "kind", "environment"},
	"runtime":           {"instance_id", "platform_name", "platform_kind", "environment", "materialization_status"},
	"cloud_dependencies": {
		"id", "name", "kind", "resource_type", "confidence", "relationship_basis",
		"candidate_status", "missing_relationship", "match_basis", "service_anchor_status", "service_anchor_reason",
	},
}

// storyTraceFallbackHandleKeys is the union of identity keys for a trace
// segment this file does not know by name.
var storyTraceFallbackHandleKeys = []string{
	"id", "resolved_id", "relationship_type", "instance_id", "path", "methods",
	"type", "name", "kind", "hostname", "target", "environment", "platform_name",
}

// storyEvidenceSummaryShippedAt is the pointer key a ci_cd handle row carries
// in place of an evidence_summary the top-level ci_cd_evidence already holds.
const storyEvidenceSummaryShippedAt = "evidence_summary_shipped_at"

// storyRepositoryHandleKeys keep a repository row of the investigation packet.
var storyRepositoryHandleKeys = []string{"repo_id", "repo_name"}

// storyGraphEdgeHandleKeys keep an evidence_graph edge down to its endpoints,
// relationship type, confidence, and resolved_id, the key
// get_relationship_evidence takes.
var storyGraphEdgeHandleKeys = []string{"id", "source", "target", "relationship_type", "resolved_id", "confidence"}

// storyGraphNodeHandleKeys keep an evidence_graph node to its identity and role.
var storyGraphNodeHandleKeys = []string{"id", "label", "kind", "role"}

// ApplyStoryEvidenceDetail shapes a service-story payload in place after the
// dossier is built, so every count, id, status, truth label, and freshness
// field is computed from the full rows and the shaping changes only what ships
// (#8798). An empty detail means full. It always sets evidence_detail.
//
// Under handles it first applies the same projection the service and workload
// context routes use (ApplyContextEvidenceDetail: deployment_evidence.artifacts
// and api_surface.endpoints to identity rows, content-derived deployment
// evidence dropped), then projects the other bulky families:
// code_to_runtime_trace segment evidence, the upstream and downstream
// relationship lists, the investigation repositories, and the evidence_graph
// nodes and edges. Every projected list keeps its length, so a count never
// disagrees with its list; the bulk that goes is the per-row detail.
//
// It returns one truth omission per reduced family with the rows it held, and
// sets evidence_detail_drilldown to say how to fetch the full rows. Rows are
// projected into new maps, so a map shared with a read model is never mutated.
// Call ValidateContextEvidenceDetail first.
func ApplyStoryEvidenceDetail(data map[string]any, detail string) []TruthOmission {
	omissions := ApplyContextEvidenceDetail(data, detail)
	if data["evidence_detail"] != ContextEvidenceDetailHandles {
		return nil
	}
	correctStoryArtifactTotal(data, omissions)
	for _, list := range storyListHandleKeys {
		if omission, ok := projectStoryList(data, "", list.key, list.keys); ok {
			omissions = append(omissions, omission)
		}
	}
	omissions = append(omissions, projectStoryDownstream(data)...)
	omissions = append(omissions, projectStoryTrace(data)...)
	omissions = append(omissions, projectStoryInvestigation(data)...)
	omissions = append(omissions, projectStoryEvidenceGraph(data)...)
	if len(omissions) == 0 {
		return nil
	}
	data["evidence_detail_drilldown"] = map[string]any{
		"full_rows": fmt.Sprintf(
			"repeat the call with evidence_detail full for every field of every row; "+
				"under handles each list keeps its length and drops per-row detail, "+
				"and rows past the %d shipped need full", ContextStoryItemLimit),
		"artifact_tool": "get_relationship_evidence",
		"artifact_key":  "resolved_id",
	}
	return omissions
}

// projectStoryList replaces parent[key] (or data[key] when parentKey is empty)
// with identity rows. The section name is the dotted path of the list. It
// reports false when the list is absent or empty. A trailing truncation marker
// row (repo_id __truncated__) is kept whole, with its note, and is not counted
// as a repository.
func projectStoryList(data map[string]any, parentKey string, key string, keys []string) (TruthOmission, bool) {
	holder, section := data, key
	if parentKey != "" {
		holder, section = MapValue(data, parentKey), parentKey+"."+key
	}
	rows := MapSliceValue(holder, key)
	if len(rows) == 0 {
		return TruthOmission{}, false
	}
	var marker []map[string]any
	if last := rows[len(rows)-1]; StringVal(last, "repo_id") == storyTruncationMarkerRepoID {
		marker, rows = rows[len(rows)-1:], rows[:len(rows)-1]
	}
	projected := append(projectContextRows(rows, keys), marker...)
	if parentKey == "" {
		data[key] = projected
	} else {
		shaped := CopyMap(holder)
		shaped[key] = projected
		data[parentKey] = shaped
	}
	return TruthOmission{Section: section, Detail: ContextEvidenceDetailHandles, Total: storyListTotal(data, parentKey, key, len(rows))}, true
}

// storyTruncationMarkerRepoID is the repo_id of the row the investigation
// packet appends when it cut its repository list.
const storyTruncationMarkerRepoID = "__truncated__"

// storyListTotal returns the true total of a story list: the larger of the
// shipped row count and the count the story recorded before it capped the list.
// The story records that count in a different place per family: upstream
// dependencies in result_limits.upstream_count, the other top-level lists in
// raw_context_limits.<key>.count. The investigation packet records none, so its
// shipped (non-marker) row count is all it knows.
func storyListTotal(data map[string]any, parentKey string, key string, shipped int) int {
	switch {
	case parentKey != "":
		return shipped
	case key == "upstream_dependencies":
		return max(shipped, IntVal(MapValue(data, "result_limits"), "upstream_count"))
	default:
		return max(shipped, IntVal(MapValue(MapValue(data, "raw_context_limits"), key), "count"))
	}
}

// correctStoryArtifactTotal raises the deployment_evidence.artifacts omission
// to the story's own pre-cap count. ApplyContextEvidenceDetail reads
// result_limits.artifact_count, which only the context routes carry; the story
// records the count in deployment_evidence.artifact_count and
// raw_context_limits.deployment_evidence.artifacts.count.
func correctStoryArtifactTotal(data map[string]any, omissions []TruthOmission) {
	evidence := MapValue(data, "deployment_evidence")
	rawLimits := MapValue(MapValue(MapValue(data, "raw_context_limits"), "deployment_evidence"), "artifacts")
	for i := range omissions {
		if omissions[i].Section == "deployment_evidence.artifacts" {
			omissions[i].Total = max(omissions[i].Total, IntVal(evidence, "artifact_count"), IntVal(rawLimits, "count"))
		}
	}
}

// projectStoryDownstream drops the downstream_consumers row lists that repeat
// the top-level dependents and consumer_repositories lists. The counts and the
// truncated flag stay, and each dropped list is named by <list>_shipped_at.
func projectStoryDownstream(data map[string]any) []TruthOmission {
	downstream := MapValue(data, "downstream_consumers")
	if len(downstream) == 0 {
		return nil
	}
	var (
		shaped    map[string]any
		omissions []TruthOmission
	)
	for _, list := range storyDownstreamShippedAt {
		rows := MapSliceValue(downstream, list.rowsKey)
		if len(rows) == 0 {
			continue
		}
		if shaped == nil {
			shaped = CopyMap(downstream)
		}
		delete(shaped, list.rowsKey)
		shaped[list.rowsKey+"_shipped_at"] = list.shippedAt
		omissions = append(omissions, TruthOmission{
			Section: "downstream_consumers." + list.rowsKey,
			Detail:  contextEvidenceOmitted,
			Total:   max(len(rows), IntVal(downstream, list.countKey)),
		})
	}
	if shaped != nil {
		data["downstream_consumers"] = shaped
	}
	return omissions
}

// projectStoryTrace projects each code_to_runtime_trace segment's evidence rows
// to identity keys. evidence_count, status, and basis stay. A segment whose rows
// repeat a top-level section keeps a preview of storyTracePreviewLimit handles;
// the cut sets truncated, evidence_shipped, and evidence_omitted. The ci_cd
// evidence_summary is dropped when the top-level ci_cd_evidence carries the
// same value.
func projectStoryTrace(data map[string]any) []TruthOmission {
	trace := MapValue(data, "code_to_runtime_trace")
	segments := MapSliceValue(trace, "segments")
	if len(segments) == 0 {
		return nil
	}
	var omissions []TruthOmission
	shapedSegments := make([]map[string]any, 0, len(segments))
	for _, segment := range segments {
		shaped := CopyMap(segment)
		rows := MapSliceValue(segment, "evidence")
		name := StringVal(segment, "name")
		if len(rows) > 0 {
			kept := rows
			if storyTraceDuplicatedSegments[name] && len(rows) > storyTracePreviewLimit {
				kept = rows[:storyTracePreviewLimit]
			}
			shaped["evidence"] = projectStoryTraceRows(name, kept, len(MapValue(data, "ci_cd_evidence")) > 0)
			if len(kept) < len(rows) {
				shaped["truncated"] = true
				shaped["evidence_shipped"] = len(kept)
				shaped["evidence_omitted"] = max(len(rows), IntVal(segment, "evidence_count")) - len(kept)
			}
			omissions = append(omissions, TruthOmission{
				Section: "code_to_runtime_trace." + name + ".evidence",
				Detail:  ContextEvidenceDetailHandles,
				Total:   max(len(rows), IntVal(segment, "evidence_count")),
			})
		}
		if _, has := segment["evidence_summary"]; has && len(MapValue(data, "ci_cd_evidence")) > 0 {
			delete(shaped, "evidence_summary")
		}
		shapedSegments = append(shapedSegments, shaped)
	}
	shapedTrace := CopyMap(trace)
	shapedTrace["segments"] = shapedSegments
	data["code_to_runtime_trace"] = shapedTrace
	return omissions
}

// projectStoryTraceRows projects one segment's evidence rows to that segment's
// handle keys. A ci_cd row that only wraps an evidence_summary the top-level
// ci_cd_evidence already carries becomes a pointer to it, so the row is never
// empty.
func projectStoryTraceRows(name string, rows []map[string]any, ciCDShipped bool) []map[string]any {
	keys, known := storyTraceHandleKeys[name]
	if !known {
		keys = storyTraceFallbackHandleKeys
	}
	projected := projectContextRows(rows, keys)
	if name != "ci_cd" || !ciCDShipped {
		return projected
	}
	for i, row := range rows {
		if _, has := row["evidence_summary"]; has {
			projected[i][storyEvidenceSummaryShippedAt] = "ci_cd_evidence"
		}
	}
	return projected
}

func projectStoryInvestigation(data map[string]any) []TruthOmission {
	var omissions []TruthOmission
	for _, key := range []string{"repositories_considered", "repositories_with_evidence"} {
		if omission, ok := projectStoryList(data, "investigation", key, storyRepositoryHandleKeys); ok {
			omissions = append(omissions, omission)
		}
	}
	return omissions
}

// projectStoryEvidenceGraph projects the shipped edges to identity keys and
// keeps only the nodes a shipped edge touches, plus the workload and its source
// repository. The node list has no row cap of its own, so under handles it is a
// cut: node_count keeps the total, nodes_truncated says whether nodes were
// left out, and nodes_omitted counts them. Full returns every node.
func projectStoryEvidenceGraph(data map[string]any) []TruthOmission {
	graph := MapValue(data, "evidence_graph")
	if len(graph) == 0 {
		return nil
	}
	shaped := CopyMap(graph)
	var omissions []TruthOmission
	edges := MapSliceValue(graph, "edges")
	if len(edges) > 0 {
		shaped["edges"] = projectContextRows(edges, storyGraphEdgeHandleKeys)
		omissions = append(omissions, TruthOmission{
			Section: "evidence_graph.edges",
			Detail:  ContextEvidenceDetailHandles,
			Total:   max(len(edges), IntVal(graph, "edge_count")),
		})
	}
	nodes := MapSliceValue(graph, "nodes")
	if len(nodes) > 0 {
		touched := make(map[string]struct{}, 2*len(edges))
		for _, edge := range edges {
			touched[StringVal(edge, "source")] = struct{}{}
			touched[StringVal(edge, "target")] = struct{}{}
		}
		kept := make([]map[string]any, 0, len(nodes))
		for _, node := range nodes {
			_, onEdge := touched[StringVal(node, "id")]
			if onEdge || StringVal(node, "role") == "workload" || StringVal(node, "role") == "source_repository" {
				kept = append(kept, node)
			}
		}
		shaped["nodes"] = projectContextRows(kept, storyGraphNodeHandleKeys)
		shaped["node_count"] = len(nodes)
		shaped["nodes_truncated"] = len(kept) < len(nodes)
		shaped["nodes_omitted"] = len(nodes) - len(kept)
		omissions = append(omissions, TruthOmission{
			Section: "evidence_graph.nodes",
			Detail:  ContextEvidenceDetailHandles,
			Total:   len(nodes),
		})
	}
	data["evidence_graph"] = shaped
	return omissions
}
