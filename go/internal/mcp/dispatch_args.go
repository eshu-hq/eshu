package mcp

import "strings"

func stringSlice(args map[string]any, key string) []any {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	values, ok := raw.([]any)
	if ok {
		return values
	}
	stringValues, ok := raw.([]string)
	if !ok {
		return nil
	}
	result := make([]any, 0, len(stringValues))
	for _, value := range stringValues {
		result = append(result, value)
	}
	return result
}

func firstString(values []any) string {
	if len(values) == 0 {
		return ""
	}
	value, _ := values[0].(string)
	return value
}

func normalizeQualifiedIdentifier(value string) string {
	if head, tail, ok := strings.Cut(value, ":"); ok && head != "" && tail != "" {
		return tail
	}
	return value
}
