package reducer

import "strings"

func codeCallEntityTypeForBucket(bucket string) string {
	switch bucket {
	case "classes":
		return "Class"
	case "structs":
		return "Struct"
	case "interfaces":
		return "Interface"
	case "type_aliases":
		return "TypeAlias"
	default:
		return ""
	}
}

func codeCallEndpointEntityType(index codeEntityIndex, repositoryID string, entityID string) string {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return ""
	}
	if entityType := index.entityTypeByID[entityID]; entityType != "" {
		return entityType
	}
	if repositoryID != "" && strings.HasPrefix(entityID, repositoryID+":") {
		return "File"
	}
	return ""
}
