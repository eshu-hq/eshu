package cypher

import (
	"fmt"
	"strings"
)

func canonicalEntityRowNeedsSingletonFallback(label string, row map[string]any) bool {
	return canonicalEntityValueContainsSubstring(row, "shortestpath") ||
		canonicalEntityValueContainsSubstring(row, "allshortestpaths")
}

func canonicalEntitySingletonFallbackMode(label string, row map[string]any) string {
	return PhaseGroupModeExecuteOnly
}

func canonicalEntitySingletonFallbackName(mode string) string {
	if mode == PhaseGroupModeGroupedSingleton {
		return "grouped_singleton"
	}
	return "singleton_parameterized"
}

func canonicalEntityValueContainsSubstring(value any, needle string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(strings.ToLower(typed), needle)
	case []string:
		for _, item := range typed {
			if canonicalEntityValueContainsSubstring(item, needle) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if canonicalEntityValueContainsSubstring(item, needle) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			if canonicalEntityValueContainsSubstring(key, needle) || canonicalEntityValueContainsSubstring(item, needle) {
				return true
			}
		}
	}
	return false
}

func canonicalNodeEntitySingletonWithContainmentStatement(
	label string,
	filePath string,
	row map[string]any,
	summary string,
	scopeID string,
	generationID string,
) Statement {
	mode := canonicalEntitySingletonFallbackMode(label, row)
	if summary == "" {
		summary = fmt.Sprintf(
			"label=%s rows=1 entity_id=%v fallback=%s containment=inline",
			label,
			row["entity_id"],
			canonicalEntitySingletonFallbackName(mode),
		)
	}
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    fmt.Sprintf(canonicalNodeEntitySingletonUpsertWithContainmentTemplate, label),
		Parameters: map[string]any{
			"file_path":                        filePath,
			"entity_id":                        row["entity_id"],
			"props":                            row["props"],
			"generation_id":                    row["generation_id"],
			StatementMetadataPhaseKey:          CanonicalPhaseEntities,
			StatementMetadataEntityLabelKey:    label,
			StatementMetadataPhaseGroupModeKey: mode,
			StatementMetadataSummaryKey:        summary,
			StatementMetadataScopeIDKey:        scopeID,
			StatementMetadataGenerationIDKey:   generationID,
		},
	}
}
