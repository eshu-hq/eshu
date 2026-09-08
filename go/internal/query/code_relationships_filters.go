// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Relationship response filters, split out of code_relationships.go for the
// 500-line file cap (#6060). These shape an already-fetched relationship set:
// direction normalisation, type filtering, and row coercion. None of them
// reads the graph, and none is pinned by the queryplan source-coverage
// manifest -- the two pinned readers, relationshipsGraphRow and
// transitiveRelationshipsGraphRow, deliberately stay in code_relationships.go
// so their pinned source text is untouched by this split.

func normalizeRelationshipDirection(direction string) (string, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(direction)); normalized {
	case "", "incoming", "outgoing":
		return normalized, nil
	default:
		return "", errors.New("direction must be incoming or outgoing")
	}
}

func filterRelationshipResponse(
	response map[string]any,
	direction string,
	relationshipType string,
) map[string]any {
	filtered := make(map[string]any, len(response))
	for key, value := range response {
		filtered[key] = value
	}

	outgoing := filterRelationships(mapRelationships(response["outgoing"]), relationshipType)
	incoming := filterRelationships(mapRelationships(response["incoming"]), relationshipType)
	if direction == "incoming" {
		outgoing = []map[string]any{}
	}
	if direction == "outgoing" {
		incoming = []map[string]any{}
	}

	filtered["outgoing"] = outgoing
	filtered["incoming"] = incoming
	return filtered
}

func filterRelationships(relationships []map[string]any, relationshipType string) []map[string]any {
	if len(relationships) == 0 {
		return []map[string]any{}
	}
	if relationshipType == "" {
		return relationships
	}

	filtered := make([]map[string]any, 0, len(relationships))
	for _, relationship := range relationships {
		if strings.EqualFold(StringVal(relationship, "type"), relationshipType) {
			filtered = append(filtered, relationship)
		}
	}
	return filtered
}

func mapRelationships(value any) []map[string]any {
	relationships, ok := value.([]map[string]any)
	if ok {
		return relationships
	}
	return querycontract.FilterNullRelationships(value)
}
