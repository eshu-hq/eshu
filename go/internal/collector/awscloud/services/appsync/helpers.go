// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appsync

import (
	"strings"
	"time"
)

// firstNonEmpty returns the first trimmed-non-empty value, or "" when all are
// empty.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// cloneStrings returns a trimmed copy of the input slice with empty entries
// dropped, or nil when nothing remains.
func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// cloneStringMap returns a shallow copy of the tag map, or nil when empty.
func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	output := make(map[string]string, len(values))
	for key, value := range values {
		output[key] = value
	}
	return output
}

// timeOrNil returns the RFC3339 timestamp string for non-zero times, or nil so
// emitted attributes omit unset timestamps.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}

// dataSourceResourceID builds a stable resource_id for an AppSync data source.
// Data source names are unique only within an API, so the API ID prefixes the
// name.
func dataSourceResourceID(apiID, name string) string {
	apiID = strings.TrimSpace(apiID)
	name = strings.TrimSpace(name)
	if apiID == "" || name == "" {
		return ""
	}
	return apiID + "/datasources/" + name
}

// resolverResourceID builds a stable resource_id for an AppSync resolver. A
// resolver is uniquely identified within an API by its type name and field
// name.
func resolverResourceID(apiID, typeName, fieldName string) string {
	apiID = strings.TrimSpace(apiID)
	typeName = strings.TrimSpace(typeName)
	fieldName = strings.TrimSpace(fieldName)
	if apiID == "" || typeName == "" || fieldName == "" {
		return ""
	}
	return apiID + "/types/" + typeName + "/resolvers/" + fieldName
}

// functionResourceID builds a stable resource_id for an AppSync pipeline
// function, preferring the function ID and falling back to the name.
func functionResourceID(apiID, functionID, name string) string {
	apiID = strings.TrimSpace(apiID)
	identifier := firstNonEmpty(functionID, name)
	if apiID == "" || identifier == "" {
		return ""
	}
	return apiID + "/functions/" + identifier
}

// schemaResourceID builds a stable resource_id for AppSync schema metadata. One
// schema exists per API.
func schemaResourceID(apiID string) string {
	apiID = strings.TrimSpace(apiID)
	if apiID == "" {
		return ""
	}
	return apiID + "/schema"
}

// apiKeyResourceID builds a stable resource_id for AppSync API key metadata.
func apiKeyResourceID(apiID, keyID string) string {
	apiID = strings.TrimSpace(apiID)
	keyID = strings.TrimSpace(keyID)
	if apiID == "" || keyID == "" {
		return ""
	}
	return apiID + "/apikeys/" + keyID
}

// resolverName builds a human-readable name for a resolver from its type and
// field.
func resolverName(resolver Resolver) string {
	typeName := strings.TrimSpace(resolver.TypeName)
	fieldName := strings.TrimSpace(resolver.FieldName)
	switch {
	case typeName != "" && fieldName != "":
		return typeName + "." + fieldName
	case fieldName != "":
		return fieldName
	default:
		return typeName
	}
}
