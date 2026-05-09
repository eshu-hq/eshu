package reducer

import "fmt"

// codeCallRelationshipType maps parser call-like metadata to the canonical
// relationship that truthfully describes the edge.
func codeCallRelationshipType(edge map[string]any) string {
	switch anyToString(edge["call_kind"]) {
	case "go.composite_literal_type_reference":
		return "REFERENCES"
	case "javascript.hapi_route_handler_reference":
		return "REFERENCES"
	case "javascript.function_value_reference":
		return "REFERENCES"
	case "java.method_reference":
		return "REFERENCES"
	case "typescript.type_reference":
		return "REFERENCES"
	case "python.class_reference":
		return "REFERENCES"
	default:
		return ""
	}
}

// codeCallRowKey deduplicates type references by entity pair because repeated
// literal sites do not carry distinct reachability truth.
func codeCallRowKey(repositoryID string, callerID string, calleeID string, relationshipType string, line int) string {
	if relationshipType == "REFERENCES" {
		return repositoryID + "|" + callerID + "|" + calleeID + "|" + relationshipType
	}
	return repositoryID + "|" + callerID + "|" + calleeID + "|" + fmt.Sprintf("%d", line)
}
