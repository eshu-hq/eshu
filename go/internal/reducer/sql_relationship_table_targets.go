// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// appendSQLTableTargetRows resolves a bounded entity-metadata target list to
// canonical SqlTable endpoints, appends exact-label edge rows, and reports
// missing versus ambiguous targets without guessing.
func appendSQLTableTargetRows(
	rows []map[string]any,
	seenEdges map[string]struct{},
	entityByName map[string][]sqlRelationshipEntity,
	targetNames []string,
	source sqlRelationshipEntity,
	relationshipType string,
) ([]map[string]any, int, int) {
	unresolved := 0
	ambiguous := 0
	for _, targetName := range targetNames {
		target, targetAmbiguous, ok := resolveSQLMigrationTarget(
			entityByName,
			targetName,
			"SqlTable",
			source.repoID,
			source.relativePath,
		)
		if targetAmbiguous {
			ambiguous++
			continue
		}
		if !ok {
			unresolved++
			continue
		}
		edgeKey := source.entityID + "->" + relationshipType + "->" + target.entityID
		if _, seen := seenEdges[edgeKey]; seen {
			continue
		}
		seenEdges[edgeKey] = struct{}{}
		rows = append(rows, map[string]any{
			"source_entity_id":   source.entityID,
			"target_entity_id":   target.entityID,
			"source_entity_type": source.entityType,
			"target_entity_type": target.entityType,
			"source_path":        source.path,
			"repo_id":            source.repoID,
			"relationship_type":  relationshipType,
		})
	}
	return rows, unresolved, ambiguous
}
