// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func entityMapResponse(
	req entityMapRequest,
	resolution map[string]any,
	relationships []map[string]any,
	truncated bool,
) map[string]any {
	status := StringVal(resolution, "status")
	if status == "resolved" {
		status = "mapped"
	}
	sections := entityMapSections(relationships)
	return map[string]any{
		"status":     status,
		"command":    "map_from",
		"from":       req.responseFrom(),
		"scope":      entityMapScope(req),
		"resolution": resolution,
		"sections":   sections,
		"evidence": map[string]any{
			"relationships":       relationships,
			"relationship_count":  len(relationships),
			"truncated":           truncated || BoolVal(resolution, "truncated"),
			"relationship_filter": req.Relationship,
		},
		"coverage": map[string]any{
			"query_shape": entityMapQueryShape(req),
			"depth":       req.Depth,
			"limit":       req.Limit,
			"truncated":   truncated || BoolVal(resolution, "truncated"),
		},
		"warnings": entityMapWarnings(status),
	}
}

func entityMapQueryShape(req entityMapRequest) string {
	if req.Depth <= 1 {
		return "typed_entity_map_relationship_family"
	}
	return "typed_entity_map_bounded_relationship_family"
}

func entityMapScope(req entityMapRequest) map[string]any {
	scope := compactStringMap(map[string]any{
		"from_type":    req.FromType,
		"repo_id":      req.RepoID,
		"environment":  req.Environment,
		"relationship": req.Relationship,
	})
	if req.responseFrom() != req.From {
		scope["normalized_from"] = req.From
	}
	return scope
}

func entityMapWarnings(status string) []string {
	switch status {
	case "no_match":
		return []string{"no typed entity matched the selector; no graph traversal was run"}
	case "ambiguous":
		return []string{"selector matched multiple typed entities; narrow it with --type, --repo, or --env"}
	default:
		return nil
	}
}

func entityMapSections(relationships []map[string]any) map[string]any {
	sections := map[string]any{
		"defined_by":  []any{},
		"deployed_by": []any{},
		"runs_as":     []any{},
		"depends_on":  []any{},
		"consumed_by": []any{},
	}
	for _, relationship := range relationships {
		if entityMapIsDefinedBy(relationship) {
			sections["defined_by"] = append(sections["defined_by"].([]any), relationship)
		}
		if entityMapIsDeployedBy(relationship) {
			sections["deployed_by"] = append(sections["deployed_by"].([]any), relationship)
		}
		if entityMapIsRunsAs(relationship) {
			sections["runs_as"] = append(sections["runs_as"].([]any), relationship)
		}
		if entityMapIsDependsOn(relationship) {
			sections["depends_on"] = append(sections["depends_on"].([]any), relationship)
		}
		if entityMapIsConsumedBy(relationship) {
			sections["consumed_by"] = append(sections["consumed_by"].([]any), relationship)
		}
	}
	return sections
}

func entityMapIsDefinedBy(row map[string]any) bool {
	relType := StringVal(row, "relationship_type")
	return StringVal(row, "direction") == "incoming" &&
		(relType == "DEFINES" || relType == "CONTAINS" || relType == "REPO_CONTAINS")
}

func entityMapIsDeployedBy(row map[string]any) bool {
	relType := StringVal(row, "relationship_type")
	return StringVal(row, "direction") == "incoming" &&
		(relType == "DEPLOYS_FROM" || relType == "HAS_DEPLOYMENT_EVIDENCE")
}

func entityMapIsRunsAs(row map[string]any) bool {
	relType := StringVal(row, "relationship_type")
	labels := StringSliceVal(row, "entity_labels")
	if relType == "RUNS_ON" || relType == "INSTANCE_OF" {
		return true
	}
	return relType == "USES" && (hasEntityMapLabel(labels, "CloudResource") || hasEntityMapLabel(labels, "K8sResource"))
}

func entityMapIsDependsOn(row map[string]any) bool {
	if StringVal(row, "direction") != "outgoing" {
		return false
	}
	switch StringVal(row, "relationship_type") {
	case "DEPENDS_ON", "USES", "USES_MODULE", "PROVISIONS_DEPENDENCY_FOR", "READS_CONFIG_FROM", "CALLS", "IMPORTS", "RUNS_ON":
		return true
	default:
		return false
	}
}

func entityMapIsConsumedBy(row map[string]any) bool {
	if StringVal(row, "direction") != "incoming" {
		return false
	}
	return !entityMapIsDefinedBy(row) && !entityMapIsDeployedBy(row)
}

func hasEntityMapLabel(labels []string, label string) bool {
	for _, candidate := range labels {
		if candidate == label {
			return true
		}
	}
	return false
}
