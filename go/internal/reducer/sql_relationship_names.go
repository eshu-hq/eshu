// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// SQLRelationshipRowStats reports READS_FROM target resolution outcomes that
// ExtractSQLRelationshipRows could not turn into an edge, so the caller can
// log them for operator visibility instead of silently dropping them (#5345).
type SQLRelationshipRowStats struct {
	// UnresolvedReadTargets counts source_tables entries that matched no
	// in-repo SqlTable or SqlView, even after the unqualified-name fallback.
	UnresolvedReadTargets int
	// AmbiguousReadTargets counts source_tables entries that matched both a
	// SqlTable and a SqlView under the same name; the resolver refuses to
	// guess which one the read targets and skips the edge.
	AmbiguousReadTargets int
}

// resolveSQLReadTarget resolves one READS_FROM target name for a SqlView or
// SqlFunction source. It tries SqlTable first, then SqlView, so a view-on-view
// direct read resolves to the upstream view. If name matches both a SqlTable
// and a SqlView, resolution is ambiguous and the caller must not guess (#5345).
//
// On a full miss, it retries once with the unqualified (schema-stripped) form
// of name, so a qualified mention (e.g. "public.orders") still resolves
// against a bare definition (e.g. "orders") that resolveSQLRelationshipTarget's
// exact-key lookup would otherwise miss.
func resolveSQLReadTarget(
	entityByName map[string][]sqlRelationshipEntity,
	name string,
	repoID string,
	relativePath string,
) (target sqlRelationshipEntity, ambiguous bool, ok bool) {
	if target, ambiguous, ok = resolveSQLReadTargetExact(entityByName, name, repoID, relativePath); ok || ambiguous {
		return target, ambiguous, ok
	}
	if unqualified := unqualifiedSQLRelationshipName(name); unqualified != "" && unqualified != name {
		return resolveSQLReadTargetExact(entityByName, unqualified, repoID, relativePath)
	}
	return sqlRelationshipEntity{}, false, false
}

// resolveSQLReadTargetExact resolves name against SqlTable and SqlView
// candidates without the unqualified-name fallback.
func resolveSQLReadTargetExact(
	entityByName map[string][]sqlRelationshipEntity,
	name string,
	repoID string,
	relativePath string,
) (sqlRelationshipEntity, bool, bool) {
	tableTarget, tableOK := resolveSQLRelationshipTarget(entityByName, name, "SqlTable", repoID, relativePath)
	viewTarget, viewOK := resolveSQLRelationshipTarget(entityByName, name, "SqlView", repoID, relativePath)
	switch {
	case tableOK && viewOK:
		return sqlRelationshipEntity{}, true, false
	case tableOK:
		return tableTarget, false, true
	case viewOK:
		return viewTarget, false, true
	default:
		return sqlRelationshipEntity{}, false, false
	}
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
