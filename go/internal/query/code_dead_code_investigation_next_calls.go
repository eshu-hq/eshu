package query

import "strings"

func deadCodeInvestigationNextCalls(scan deadCodeInvestigationScan) []map[string]any {
	candidates := append([]map[string]any{}, scan.CleanupReady...)
	candidates = append(candidates, scan.Ambiguous...)
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}
	next := make([]map[string]any, 0, len(candidates)*4)
	for _, candidate := range candidates {
		entityID := StringVal(candidate, "entity_id")
		if entityID == "" {
			continue
		}
		next = append(next, map[string]any{
			"tool":      "get_entity_content",
			"arguments": map[string]any{"entity_id": entityID},
			"reason":    "read the exact source before changing or deleting the candidate",
		})
		for _, relationshipType := range deadCodeInvestigationRelationshipTypes(candidate) {
			next = append(next, deadCodeInvestigationRelationshipCall(entityID, relationshipType))
		}
		if primaryEntityLabel(candidate) == "SqlFunction" {
			next = append(next, deadCodeInvestigationSQLExecuteCall(candidate))
		}
	}
	return next
}

func deadCodeInvestigationRelationshipTypes(candidate map[string]any) []string {
	switch primaryEntityLabel(candidate) {
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

func deadCodeInvestigationRelationshipCall(entityID string, relationshipType string) map[string]any {
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

func deadCodeInvestigationSQLExecuteCall(candidate map[string]any) map[string]any {
	entityID := StringVal(candidate, "entity_id")
	return map[string]any{
		"tool": "execute_cypher_query",
		"arguments": map[string]any{
			"cypher_query": "MATCH (e:SqlFunction {uid: " + deadCodeCypherStringLiteral(entityID) + "})<-[:EXECUTES]-(source) RETURN coalesce(source.uid, source.id) as source_id, labels(source) as source_labels LIMIT 25",
			"limit":        25,
		},
		"reason": "SQL routine reachability uses EXECUTES edges, which relationship-story does not expose",
	}
}

func deadCodeCypherStringLiteral(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value)
	return "'" + escaped + "'"
}
