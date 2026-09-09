// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// The relationship response filters split out of code_relationships.go
// (#6060): they shape the handler payload after the graph rows resolve,
// so they live apart from the row readers.

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
		if strings.EqualFold(querycontract.StringVal(relationship, "type"), relationshipType) {
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
