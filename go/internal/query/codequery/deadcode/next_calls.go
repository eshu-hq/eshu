// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// InvestigationNextCalls suggests up to five candidates' worth of
// follow-up tool calls for a dead-code investigation scan: read the
// exact source first, then check incoming evidence per relationship
// type the candidate's label admits, plus an EXECUTES reachability read
// for SQL routines (which relationship-story does not expose).
func InvestigationNextCalls(scan DeadCodeInvestigationScan) []map[string]any {
	candidates := append([]map[string]any{}, scan.CleanupReady...)
	candidates = append(candidates, scan.Ambiguous...)
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}
	next := make([]map[string]any, 0, len(candidates)*4)
	for _, candidate := range candidates {
		entityID := querycontract.StringVal(candidate, "entity_id")
		if entityID == "" {
			continue
		}
		next = append(next, map[string]any{
			"tool":      "get_entity_content",
			"arguments": map[string]any{"entity_id": entityID},
			"reason":    "read the exact source before changing or deleting the candidate",
		})
		for _, relationshipType := range investigationRelationshipTypes(candidate) {
			next = append(next, investigationRelationshipCall(entityID, relationshipType))
		}
		if querycontract.PrimaryEntityLabel(candidate) == "SqlFunction" {
			next = append(next, investigationSQLExecuteCall(candidate))
		}
	}
	return next
}

// investigationRelationshipTypes lists the incoming-evidence
// relationship types admitted for a candidate's primary label.
func investigationRelationshipTypes(candidate map[string]any) []string {
	switch querycontract.PrimaryEntityLabel(candidate) {
	case "Function":
		return []string{"CALLS", "REFERENCES", "IMPORTS"}
	case "Class", "Struct":
		return []string{"REFERENCES", "INHERITS"}
	case "Interface", "Trait":
		return []string{"REFERENCES", "INHERITS", "OVERRIDES"}
	case "SqlFunction":
		return nil
	default:
		return []string{"REFERENCES"}
	}
}

// investigationRelationshipCall shapes one incoming-evidence check call.
func investigationRelationshipCall(entityID string, relationshipType string) map[string]any {
	return map[string]any{
		"tool": "get_code_relationship_story",
		"arguments": map[string]any{
			"entity_id":          entityID,
			"direction":          "incoming",
			"relationship_type":  relationshipType,
			"include_transitive": false,
			"limit":              25,
			"offset":             0,
		},
		"reason": "check incoming " + relationshipType + " evidence before treating the candidate as cleanup-ready",
	}
}

// investigationSQLExecuteCall shapes the EXECUTES reachability read for
// a SQL routine candidate.
func investigationSQLExecuteCall(candidate map[string]any) map[string]any {
	entityID := querycontract.StringVal(candidate, "entity_id")
	return map[string]any{
		"tool": "execute_cypher_query",
		"arguments": map[string]any{
			"cypher_query": "MATCH (e:SqlFunction {uid: " + cypherStringLiteral(entityID) + "})<-[:EXECUTES]-(source) RETURN coalesce(source.uid, source.id) as source_id, labels(source) as source_labels LIMIT 25",
			"limit":        25,
		},
		"reason": "SQL routine reachability uses EXECUTES edges, which relationship-story does not expose",
	}
}

// cypherStringLiteral quotes a value as a Cypher string literal.
func cypherStringLiteral(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value)
	return "'" + escaped + "'"
}
