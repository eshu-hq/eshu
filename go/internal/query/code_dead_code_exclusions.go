package query

import (
	"slices"
	"strings"
)

func filterResultsByDecoratorExclusions(results []map[string]any, excluded []string) []map[string]any {
	if len(results) == 0 || len(excluded) == 0 {
		return results
	}

	normalizedExcluded := make([]string, 0, len(excluded))
	for _, decorator := range excluded {
		if normalized := normalizeDecoratorName(decorator); normalized != "" {
			normalizedExcluded = append(normalizedExcluded, normalized)
		}
	}
	if len(normalizedExcluded) == 0 {
		return results
	}

	filtered := make([]map[string]any, 0, len(results))
	for _, result := range results {
		metadata, ok := result["metadata"].(map[string]any)
		if !ok {
			filtered = append(filtered, result)
			continue
		}
		if !resultMatchesDecoratorExclusion(metadata, normalizedExcluded) {
			filtered = append(filtered, result)
		}
	}

	return filtered
}

func resultMatchesDecoratorExclusion(metadata map[string]any, excluded []string) bool {
	rawDecorators, ok := metadata["decorators"].([]any)
	if !ok {
		return false
	}

	for _, raw := range rawDecorators {
		decorator, ok := raw.(string)
		if !ok {
			continue
		}
		if slices.Contains(excluded, normalizeDecoratorName(decorator)) {
			return true
		}
	}

	return false
}

func normalizeDecoratorName(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return strings.TrimPrefix(trimmed, "@")
}
