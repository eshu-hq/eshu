// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"errors"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// FilterResponse shapes the handler payload after the graph rows
// resolve: it filters both directions by relationship type, then drops
// the direction the caller did not ask for.
func FilterResponse(
	response map[string]any,
	direction string,
	relationshipType string,
) map[string]any {
	filtered := make(map[string]any, len(response))
	for key, value := range response {
		filtered[key] = value
	}

	outgoing := FilterRelationships(MapRelationships(response["outgoing"]), relationshipType)
	incoming := FilterRelationships(MapRelationships(response["incoming"]), relationshipType)
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

// FilterRelationships drops relationships whose type does not match,
// case-insensitively. An empty type keeps every relationship.
func FilterRelationships(relationships []map[string]any, relationshipType string) []map[string]any {
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

// MapRelationships coerces a response leg to relationship rows,
// dropping nulls the way the pre-split shape did.
func MapRelationships(value any) []map[string]any {
	relationships, ok := value.([]map[string]any)
	if ok {
		return relationships
	}
	return querycontract.FilterNullRelationships(value)
}

// Capability resolves the contract capability for one direction and
// relationship type.
func Capability(direction, relationshipType string) string {
	switch relationshipType {
	case "CALLS":
		if direction == "incoming" {
			return "call_graph.direct_callers"
		}
		return "call_graph.direct_callees"
	case "IMPORTS":
		return "symbol_graph.imports"
	case "INHERITS", "OVERRIDES":
		return "symbol_graph.inheritance"
	default:
		return "call_graph.direct_callees"
	}
}

// TransitiveCapability resolves the contract capability for transitive
// CALLS in one direction.
func TransitiveCapability(direction string) string {
	if direction == "incoming" {
		return "call_graph.transitive_callers"
	}
	return "call_graph.transitive_callees"
}

// TransitiveUnsupportedMessage renders the capability-gate message for
// transitive CALLS in one direction.
func TransitiveUnsupportedMessage(direction string) string {
	if direction == "incoming" {
		return "transitive callers require authoritative graph mode"
	}
	return "transitive callees require authoritative graph mode"
}

// NormalizeDirection coerces the direction filter the relationships
// handler accepts.
func NormalizeDirection(direction string) (string, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(direction)); normalized {
	case "", "incoming", "outgoing":
		return normalized, nil
	default:
		return "", errors.New("direction must be incoming or outgoing")
	}
}
