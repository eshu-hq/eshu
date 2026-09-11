// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

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

// EndpointEntityType returns the entity kind for entityID as recorded in
// index (Function, Class, Struct, Interface, TypeAlias), or "File" when
// entityID is not in the index but carries repositoryID as its prefix. It
// returns "" when entityID is blank or unresolvable, which callers treat as
// "not a callable Function" and skip.
func EndpointEntityType(index EntityIndex, repositoryID string, entityID string) string {
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
