package json

import (
	"fmt"
	"sort"
	"strings"
)

func completeCoverage() map[string]any {
	return map[string]any{
		"confidence":            1.0,
		"state":                 "complete",
		"unresolved_references": []string{},
	}
}

func metadataField(document map[string]any, key string) any {
	metadata, _ := document["metadata"].(map[string]any)
	if metadata == nil {
		return nil
	}
	return metadata[key]
}

func jsonObjectSlice(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	results := make([]map[string]any, 0, len(items))
	for _, item := range items {
		mapping, ok := item.(map[string]any)
		if ok {
			results = append(results, mapping)
		}
	}
	return results
}

func jsonStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	results := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(fmt.Sprint(item))
		if trimmed != "" {
			results = append(results, trimmed)
		}
	}
	return results
}

func sortJSONRecords(items []map[string]any) []map[string]any {
	sort.Slice(items, func(i, j int) bool {
		leftName, _ := items[i]["name"].(string)
		rightName, _ := items[j]["name"].(string)
		return leftName < rightName
	})
	return items
}

func sortedJSONRecords(items map[string]map[string]any) []map[string]any {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	results := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		results = append(results, items[key])
	}
	return results
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortRelationships(items []map[string]any) []map[string]any {
	sort.Slice(items, func(i, j int) bool {
		leftType, _ := items[i]["type"].(string)
		rightType, _ := items[j]["type"].(string)
		if leftType != rightType {
			return leftType < rightType
		}
		leftSource, _ := items[i]["source_name"].(string)
		rightSource, _ := items[j]["source_name"].(string)
		if leftSource != rightSource {
			return leftSource < rightSource
		}
		leftTarget, _ := items[i]["target_name"].(string)
		rightTarget, _ := items[j]["target_name"].(string)
		return leftTarget < rightTarget
	})
	return items
}

func sortedSetValues(values map[string]struct{}) []string {
	items := make([]string, 0, len(values))
	for value := range values {
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}

func defaultString(value any, fallback string) string {
	trimmed := strings.TrimSpace(fmt.Sprint(value))
	if trimmed == "" || trimmed == "<nil>" {
		return fallback
	}
	return trimmed
}

func optionalString(value any) any {
	trimmed := strings.TrimSpace(fmt.Sprint(value))
	if trimmed == "" || trimmed == "<nil>" {
		return nil
	}
	return trimmed
}

func jsonBool(value any) bool {
	boolean, ok := value.(bool)
	return ok && boolean
}

func nonEmptyStrings(values ...string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" && trimmed != "<nil>" {
			items = append(items, trimmed)
		}
	}
	return items
}
