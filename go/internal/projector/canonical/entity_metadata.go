// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}

	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = cloneAnyValue(value)
	}
	return cloned
}

func cloneAnySlice(input []any) []any {
	if input == nil {
		return nil
	}

	cloned := make([]any, len(input))
	for i, value := range input {
		cloned[i] = cloneAnyValue(value)
	}
	return cloned
}

func cloneStringSlice(input []string) []string {
	if input == nil {
		return nil
	}
	return append([]string(nil), input...)
}

func cloneAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		return cloneAnySlice(typed)
	case []string:
		return cloneStringSlice(typed)
	default:
		return typed
	}
}

var entityPayloadReservedKeys = map[string]struct{}{
	"artifact_type":    {},
	"content_body":     {},
	"content_digest":   {},
	"content_path":     {},
	"end_byte":         {},
	"end_line":         {},
	"entity_id":        {},
	"entity_kind":      {},
	"entity_metadata":  {},
	"entity_name":      {},
	"entity_type":      {},
	"graph_id":         {},
	"graph_kind":       {},
	"iac_relevant":     {},
	"indexed_at":       {},
	"lang":             {},
	"language":         {},
	"line_number":      {},
	"name":             {},
	"path":             {},
	"relative_path":    {},
	"repo_id":          {},
	"source_cache":     {},
	"sql_entity_type":  {},
	"start_byte":       {},
	"start_line":       {},
	"template_dialect": {},
}

// EntityMetadataFromPayload derives the entity metadata map for a parsed-entity
// fact. An explicit entity_metadata object in the payload wins; otherwise every
// payload key that is not a reserved structural field is carried through. Both
// paths deep-copy, so the returned map never aliases the fact payload. It
// returns nil when no metadata survives, so an entity with no metadata is stored
// as absent rather than as an empty map.
func EntityMetadataFromPayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	if raw, ok := payload["entity_metadata"]; ok {
		if typed, ok := raw.(map[string]any); ok && len(typed) > 0 {
			return cloneAnyMap(typed)
		}
	}

	metadata := make(map[string]any)
	for key, value := range payload {
		if _, reserved := entityPayloadReservedKeys[key]; reserved {
			continue
		}
		metadata[key] = cloneAnyValue(value)
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}
