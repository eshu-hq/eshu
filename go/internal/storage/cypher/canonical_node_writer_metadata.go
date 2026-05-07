package cypher

import "strings"

var canonicalEntityMetadataReservedProperties = map[string]struct{}{
	"end_line":        {},
	"evidence_source": {},
	"generation_id":   {},
	"id":              {},
	"lang":            {},
	"language":        {},
	"line_number":     {},
	"name":            {},
	"path":            {},
	"relative_path":   {},
	"repo_id":         {},
	"scope_id":        {},
	"start_line":      {},
	"uid":             {},

	// Dead-code roots are high-volume analysis metadata. The dead-code API
	// merges them from the content store by entity ID, so keeping them off the
	// canonical graph avoids list-property writes on the hot entity path.
	"dead_code_root_kinds": {},
}

func canonicalEntityMetadataProperties(row map[string]any) map[string]any {
	metadata, ok := row["entity_metadata"].(map[string]any)
	if !ok || len(metadata) == 0 {
		return nil
	}

	result := map[string]any{}
	for key, value := range metadata {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, reserved := canonicalEntityMetadataReservedProperties[key]; reserved {
			continue
		}
		if normalized, ok := canonicalGraphPropertyValue(value); ok {
			result[key] = normalized
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func canonicalGraphPropertyValue(value any) (any, bool) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil, false
		}
		return trimmed, true
	case bool:
		return typed, true
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return typed, true
	case float32:
		return typed, true
	case float64:
		return typed, true
	case []string:
		out := canonicalStringList(typed)
		return out, len(out) > 0
	case []any:
		out := canonicalAnyStringList(typed)
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func canonicalTypeScriptClassFamilyMetadata(row map[string]any) map[string]any {
	metadata, ok := row["entity_metadata"].(map[string]any)
	if !ok || len(metadata) == 0 {
		return nil
	}

	language := canonicalMetadataString(row, "language")
	if language != "typescript" && language != "tsx" {
		return nil
	}

	label := canonicalMetadataString(row, "label")
	switch label {
	case "Class", "Interface", "Enum":
	default:
		return nil
	}

	result := map[string]any{}
	if decorators := canonicalMetadataStringSlice(metadata, "decorators"); len(decorators) > 0 {
		result["decorators"] = decorators
	}
	if typeParameters := canonicalMetadataStringSlice(metadata, "type_parameters"); len(typeParameters) > 0 {
		result["type_parameters"] = typeParameters
	}
	if group := canonicalMetadataString(metadata, "declaration_merge_group"); group != "" {
		result["declaration_merge_group"] = group
	}
	if count := canonicalMetadataInt(metadata, "declaration_merge_count"); count > 0 {
		result["declaration_merge_count"] = count
	}
	if kinds := canonicalMetadataStringSlice(metadata, "declaration_merge_kinds"); len(kinds) > 0 {
		result["declaration_merge_kinds"] = kinds
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func canonicalStringList(input []string) []string {
	out := make([]string, 0, len(input))
	for _, item := range input {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func canonicalAnyStringList(input []any) []string {
	out := make([]string, 0, len(input))
	for _, item := range input {
		text, ok := item.(string)
		if !ok {
			continue
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func canonicalMetadataString(row map[string]any, key string) string {
	value, ok := row[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func canonicalMetadataStringSlice(metadata map[string]any, key string) []string {
	value, ok := metadata[key]
	if !ok || value == nil {
		return nil
	}

	switch typed := value.(type) {
	case []string:
		return canonicalStringList(typed)
	case []any:
		return canonicalAnyStringList(typed)
	default:
		return nil
	}
}

func canonicalMetadataInt(metadata map[string]any, key string) int {
	value, ok := metadata[key]
	if !ok || value == nil {
		return 0
	}

	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}
