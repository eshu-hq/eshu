// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

func semanticEvidenceRoute(toolName string, args map[string]any) (*route, bool) {
	switch toolName {
	case "list_semantic_documentation_observations":
		return &route{
			method: "GET",
			path:   "/api/v0/semantic/documentation-observations",
			query:  semanticEvidenceQuery(args, "admission_state", "observation_type", "document_id", "section_id"),
		}, true
	case "list_semantic_code_hints":
		return &route{
			method: "GET",
			path:   "/api/v0/semantic/code-hints",
			query: semanticEvidenceQuery(
				args,
				"relative_path",
				"entity_id",
				"corroboration_state",
				"hint_type",
				"relationship_kind",
			),
		}, true
	default:
		return nil, false
	}
}

func semanticEvidenceQuery(args map[string]any, extraKeys ...string) map[string]string {
	query := map[string]string{}
	keys := []string{
		"fact_id",
		"scope_id",
		"generation_id",
		"repo",
		"target_kind",
		"target_id",
		"service_id",
		"source_class",
		"source_id",
		"provider_profile_id",
		"provider_kind",
		"prompt_version",
		"redaction_version",
		"extraction_mode",
		"policy_state",
		"redaction_state",
		"freshness_state",
		"q",
		"updated_since",
		"cursor",
	}
	keys = append(keys, extraKeys...)
	for _, key := range keys {
		if value := str(args, key); value != "" {
			query[key] = value
		}
	}
	if limit := intOr(args, "limit", 50); limit > 0 {
		query["limit"] = strconv.Itoa(limit)
	}
	return query
}
