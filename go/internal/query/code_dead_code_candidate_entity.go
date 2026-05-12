package query

import "strings"

func deadCodeIsCandidateEntity(result map[string]any, entity *EntityContent) bool {
	for _, label := range StringSliceVal(result, "labels") {
		if deadCodeIsCandidateEntityType(label) {
			return true
		}
	}
	if entity == nil {
		return false
	}
	return deadCodeIsCandidateEntityType(entity.EntityType)
}

func deadCodeIsCandidateEntityType(entityType string) bool {
	switch strings.TrimSpace(entityType) {
	case "Function", "Class", "Struct", "Interface", "Trait", "SqlFunction":
		return true
	default:
		return false
	}
}
