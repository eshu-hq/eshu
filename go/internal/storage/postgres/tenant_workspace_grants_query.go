package postgres

import (
	"sort"
	"strings"
)

func normalizeGrantScopeIDs(scopeIDs []string) []string {
	seen := make(map[string]struct{}, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		scopeID = strings.TrimSpace(scopeID)
		if scopeID != "" {
			seen[scopeID] = struct{}{}
		}
	}
	normalized := make([]string, 0, len(seen))
	for scopeID := range seen {
		normalized = append(normalized, scopeID)
	}
	sort.Strings(normalized)
	return normalized
}
