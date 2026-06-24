// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func semanticEvidenceTools() []ToolDefinition {
	commonProperties := map[string]any{
		"fact_id":             map[string]any{"type": "string"},
		"scope_id":            map[string]any{"type": "string"},
		"generation_id":       map[string]any{"type": "string"},
		"repo":                map[string]any{"type": "string"},
		"target_kind":         map[string]any{"type": "string"},
		"target_id":           map[string]any{"type": "string"},
		"service_id":          map[string]any{"type": "string"},
		"source_class":        map[string]any{"type": "string"},
		"source_id":           map[string]any{"type": "string"},
		"provider_profile_id": map[string]any{"type": "string"},
		"provider_kind":       map[string]any{"type": "string"},
		"prompt_version":      map[string]any{"type": "string"},
		"redaction_version":   map[string]any{"type": "string"},
		"extraction_mode":     map[string]any{"type": "string"},
		"policy_state":        map[string]any{"type": "string"},
		"redaction_state":     map[string]any{"type": "string"},
		"freshness_state":     map[string]any{"type": "string"},
		"q":                   map[string]any{"type": "string"},
		"updated_since":       map[string]any{"type": "string"},
		"limit": map[string]any{
			"type":    "integer",
			"default": 50,
			"minimum": 1,
			"maximum": 200,
		},
		"cursor": map[string]any{"type": "string"},
	}
	documentationProperties := cloneSchemaProperties(commonProperties)
	documentationProperties["document_id"] = map[string]any{"type": "string"}
	documentationProperties["section_id"] = map[string]any{"type": "string"}
	documentationProperties["admission_state"] = map[string]any{"type": "string"}
	documentationProperties["observation_type"] = map[string]any{"type": "string"}
	codeHintProperties := cloneSchemaProperties(commonProperties)
	codeHintProperties["relative_path"] = map[string]any{"type": "string"}
	codeHintProperties["entity_id"] = map[string]any{"type": "string"}
	codeHintProperties["corroboration_state"] = map[string]any{"type": "string"}
	codeHintProperties["hint_type"] = map[string]any{"type": "string"}
	codeHintProperties["relationship_kind"] = map[string]any{"type": "string"}
	return []ToolDefinition{
		{
			Name:        "list_semantic_documentation_observations",
			Description: "List opt-in LLM-assisted documentation observations with truth basis, freshness, provider profile, prompt version, redaction version, and policy state.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": documentationProperties,
				"required":   []string{},
			},
		},
		{
			Name:        "list_semantic_code_hints",
			Description: "List opt-in non-canonical code hints with truth basis, freshness, provider profile, prompt version, redaction version, policy state, and corroboration state.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": codeHintProperties,
				"required":   []string{},
			},
		},
	}
}

func cloneSchemaProperties(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
