package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func collectSemanticMetadata(payload map[string]any) map[string]any {
	metadata := cloneAnyMap(payloadMap(payload, "entity_metadata"))
	if metadata == nil {
		metadata = make(map[string]any)
	}
	for _, key := range []string{"kind", "target_kind", "type", "trait", "target", "impl_context"} {
		if value := semanticPayloadMetadataString(payload, key); value != "" {
			metadata[key] = value
		}
	}
	for _, key := range []string{"docstring", "method_kind", "semantic_kind", "module_kind", "declaration_merge_group", "annotation_kind", "context", "enclosing_function"} {
		if value := semanticPayloadMetadataString(payload, key); value != "" {
			metadata[key] = value
		}
	}
	if classContext := semanticPayloadMetadataString(payload, "class_context"); classContext != "" {
		metadata["class_context"] = classContext
	}
	if jsxFragment := semanticPayloadMetadataBool(payload, "jsx_fragment_shorthand"); jsxFragment {
		metadata["jsx_fragment_shorthand"] = true
	}
	if componentAssertion := semanticPayloadMetadataString(payload, "component_type_assertion"); componentAssertion != "" {
		metadata["component_type_assertion"] = componentAssertion
	}
	if componentWrapper := semanticPayloadMetadataString(payload, "component_wrapper_kind"); componentWrapper != "" {
		metadata["component_wrapper_kind"] = componentWrapper
	}
	for _, key := range []string{"attribute_kind", "value"} {
		if value := semanticPayloadMetadataString(payload, key); value != "" {
			metadata[key] = value
		}
	}
	if count := semanticPayloadMetadataInt(payload, "declaration_merge_count"); count > 0 {
		metadata["declaration_merge_count"] = count
	}
	if count := semanticPayloadParameterCount(payload); count > 0 {
		metadata["parameter_count"] = count
	}
	if kinds := semanticPayloadMetadataStringSlice(payload, "declaration_merge_kinds"); len(kinds) > 0 {
		metadata["declaration_merge_kinds"] = kinds
	}
	if decorators := semanticPayloadMetadataStringSlice(payload, "decorators"); len(decorators) > 0 {
		metadata["decorators"] = decorators
	}
	if typeParameters := semanticPayloadMetadataStringSlice(payload, "type_parameters"); len(typeParameters) > 0 {
		metadata["type_parameters"] = typeParameters
	}
	if async := semanticPayloadMetadataBool(payload, "async"); async {
		metadata["async"] = true
	}
	if count, kinds := semanticPayloadTypeAnnotationSummary(payload); count > 0 {
		metadata["type_annotation_count"] = count
		if len(kinds) > 0 {
			metadata["type_annotation_kinds"] = kinds
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func payloadMap(payload map[string]any, key string) map[string]any {
	if payload == nil {
		return nil
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

func semanticPayloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	str, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(str)
}

func semanticPayloadMetadataString(payload map[string]any, key string) string {
	if value := semanticPayloadString(payload, key); value != "" {
		return value
	}
	return semanticPayloadString(payloadMap(payload, "entity_metadata"), key)
}

func semanticPayloadMetadataStringSlice(payload map[string]any, key string) []string {
	if values := semanticPayloadStringSlice(payload, key); len(values) > 0 {
		return values
	}
	return semanticPayloadStringSlice(payloadMap(payload, "entity_metadata"), key)
}

func semanticPayloadMetadataBool(payload map[string]any, key string) bool {
	if value, ok := payload[key]; ok {
		if typed, ok := value.(bool); ok {
			return typed
		}
	}
	metadata := payloadMap(payload, "entity_metadata")
	if metadata == nil {
		return false
	}
	value, ok := metadata[key]
	if !ok {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

func semanticPayloadTypeAnnotationSummary(payload map[string]any) (int, []string) {
	raw := payload["type_annotations"]
	switch typed := raw.(type) {
	case []map[string]any:
		return typeAnnotationSummaryFromMaps(typed)
	case []any:
		annotations := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			annotation, ok := item.(map[string]any)
			if !ok {
				continue
			}
			annotations = append(annotations, annotation)
		}
		return typeAnnotationSummaryFromMaps(annotations)
	default:
		return 0, nil
	}
}

func typeAnnotationSummaryFromMaps(annotations []map[string]any) (int, []string) {
	if len(annotations) == 0 {
		return 0, nil
	}

	kinds := make([]string, 0, len(annotations))
	for _, annotation := range annotations {
		if kind, _ := annotation["annotation_kind"].(string); strings.TrimSpace(kind) != "" {
			kinds = append(kinds, strings.TrimSpace(kind))
		}
	}
	if len(kinds) == 0 {
		return len(annotations), nil
	}
	return len(annotations), kinds
}

func semanticPayloadMetadataInt(payload map[string]any, key string) int {
	if value, ok := payload[key]; ok {
		switch typed := value.(type) {
		case int:
			return typed
		case int32:
			return int(typed)
		case int64:
			return int(typed)
		case float64:
			return int(typed)
		}
	}
	metadata := payloadMap(payload, "entity_metadata")
	if metadata == nil {
		return 0
	}
	value, ok := metadata[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func semanticPayloadParameterCount(payload map[string]any) int {
	if count := semanticPayloadMetadataInt(payload, "parameter_count"); count > 0 {
		return count
	}
	for _, key := range []string{"args", "parameters"} {
		if values := semanticPayloadMetadataStringSlice(payload, key); len(values) > 0 {
			return len(values)
		}
	}
	return 0
}

func semanticPayloadStringSlice(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
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
	default:
		return nil
	}
}

func isSemanticEntityType(payload map[string]any, entityType string) bool {
	switch entityType {
	case "Annotation", "Typedef", "TypeAlias", "TypeAnnotation", "Component", "Module", "ImplBlock", "Protocol", "ProtocolImplementation":
		return true
	case "Variable":
		return isElixirModuleAttributeSemanticEntity(payload) ||
			isTypeScriptJSXComponentTypeAssertionSemanticEntity(payload)
	case "Function":
		return isJavaScriptCallableSemanticEntity(payload) ||
			isGoSemanticFunction(payload) ||
			isPythonSemanticFunction(payload) ||
			isElixirSemanticFunction(payload) ||
			isRustSemanticFunction(payload) ||
			isTypeScriptSemanticFunction(payload) ||
			isTypeScriptJSXFragmentSemanticEntity(payload)
	default:
		return false
	}
}

func isJavaScriptCallableSemanticEntity(payload map[string]any) bool {
	if semanticPayloadString(payload, "language") != "javascript" {
		return false
	}
	return semanticPayloadMetadataString(payload, "docstring") != "" || semanticPayloadMetadataString(payload, "method_kind") != ""
}

func isPythonSemanticFunction(payload map[string]any) bool {
	if semanticPayloadString(payload, "language") != "python" {
		return false
	}
	if semanticPayloadMetadataString(payload, "semantic_kind") == "lambda" {
		return true
	}
	if semanticPayloadMetadataString(payload, "semantic_kind") == "generator" {
		return true
	}
	if semanticPayloadMetadataBool(payload, "async") {
		return true
	}
	if len(semanticPayloadMetadataStringSlice(payload, "decorators")) > 0 {
		return true
	}
	count, kinds := semanticPayloadTypeAnnotationSummary(payload)
	return count > 0 || len(kinds) > 0
}

func isGoSemanticFunction(payload map[string]any) bool {
	if semanticPayloadString(payload, "language") != "go" {
		return false
	}
	return hasSemanticFunctionMetadata(payload)
}

func isElixirSemanticFunction(payload map[string]any) bool {
	if semanticPayloadString(payload, "language") != "elixir" {
		return false
	}
	return semanticPayloadMetadataString(payload, "semantic_kind") == "guard"
}

func isElixirModuleAttributeSemanticEntity(payload map[string]any) bool {
	if semanticPayloadString(payload, "language") != "elixir" {
		return false
	}
	return semanticPayloadMetadataString(payload, "attribute_kind") == "module_attribute"
}

func isTypeScriptSemanticFunction(payload map[string]any) bool {
	if semanticPayloadString(payload, "language") != "typescript" {
		return false
	}
	return len(semanticPayloadMetadataStringSlice(payload, "decorators")) > 0 ||
		len(semanticPayloadMetadataStringSlice(payload, "type_parameters")) > 0
}

func isTypeScriptJSXFragmentSemanticEntity(payload map[string]any) bool {
	if semanticPayloadString(payload, "language") != "tsx" {
		return false
	}
	return semanticPayloadMetadataBool(payload, "jsx_fragment_shorthand")
}

func isTypeScriptJSXComponentTypeAssertionSemanticEntity(payload map[string]any) bool {
	if semanticPayloadString(payload, "language") != "tsx" {
		return false
	}
	return semanticPayloadMetadataString(payload, "component_type_assertion") != ""
}

func isRustSemanticFunction(payload map[string]any) bool {
	if semanticPayloadString(payload, "language") != "rust" {
		return false
	}
	return semanticPayloadMetadataString(payload, "impl_context") != "" ||
		semanticPayloadMetadataString(payload, "trait") != "" ||
		semanticPayloadMetadataString(payload, "target") != ""
}

func hasSemanticFunctionMetadata(payload map[string]any) bool {
	for _, key := range []string{
		"docstring",
		"class_context",
		"method_kind",
		"constructor_kind",
		"annotation_kind",
		"context",
		"enclosing_function",
		"impl_context",
	} {
		if semanticPayloadMetadataString(payload, key) != "" {
			return true
		}
	}
	if len(semanticPayloadMetadataStringSlice(payload, "decorators")) > 0 {
		return true
	}
	if len(semanticPayloadMetadataStringSlice(payload, "type_parameters")) > 0 {
		return true
	}
	if semanticPayloadMetadataBool(payload, "async") ||
		semanticPayloadMetadataBool(payload, "jsx_fragment_shorthand") {
		return true
	}
	count, kinds := semanticPayloadTypeAnnotationSummary(payload)
	return count > 0 || len(kinds) > 0
}

func semanticPayloadInt(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func collectSemanticRepoIDs(envelopes []facts.Envelope) []string {
	seen := make(map[string]struct{})
	repoIDs := make([]string, 0)
	for _, env := range envelopes {
		repoID := semanticPayloadString(env.Payload, "repo_id")
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		repoIDs = append(repoIDs, repoID)
	}

	sort.Strings(repoIDs)
	return repoIDs
}

func dedupeNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	deduped := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		deduped = append(deduped, value)
	}
	if len(deduped) == 0 {
		return nil
	}
	return deduped
}
