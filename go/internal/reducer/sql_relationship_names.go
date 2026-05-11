package reducer

import "strings"

func addSQLRelationshipEntityIndex(
	entityByName map[string][]sqlRelationshipEntity,
	entityName string,
	entity sqlRelationshipEntity,
) {
	entityName = strings.TrimSpace(entityName)
	if entityName == "" {
		return
	}
	entityByName[entityName] = append(entityByName[entityName], entity)
	if alias := unqualifiedSQLRelationshipName(entityName); alias != "" && alias != entityName {
		entityByName[alias] = append(entityByName[alias], entity)
	}
}

func unqualifiedSQLRelationshipName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	parts := strings.Split(name, ".")
	return strings.TrimSpace(parts[len(parts)-1])
}

func resolveSQLRelationshipTarget(
	entityByName map[string][]sqlRelationshipEntity,
	name string,
	entityType string,
	repoID string,
	relativePath string,
) (sqlRelationshipEntity, bool) {
	candidates := entityByName[name]
	if len(candidates) == 0 {
		return sqlRelationshipEntity{}, false
	}

	matching := make([]sqlRelationshipEntity, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.repoID == repoID && candidate.entityType == entityType {
			matching = append(matching, candidate)
		}
	}
	if len(matching) == 0 {
		return sqlRelationshipEntity{}, false
	}

	if relativePath != "" {
		sameFile := make([]sqlRelationshipEntity, 0, len(matching))
		for _, candidate := range matching {
			if candidate.relativePath == relativePath {
				sameFile = append(sameFile, candidate)
			}
		}
		if len(sameFile) == 1 {
			return sameFile[0], true
		}
		if len(sameFile) > 1 {
			return sqlRelationshipEntity{}, false
		}
	}

	if len(matching) == 1 {
		return matching[0], true
	}
	return sqlRelationshipEntity{}, false
}
