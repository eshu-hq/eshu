// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// dependenciesCypher builds the bounded Cypher for GET /api/v0/dependencies.
//
// The graph anchors on the indexed Package label (package_normalized_name index
// on Package.normalized_name, nornicdb_package_uid_lookup on Package.uid) and
// walks the package-native dependency chain:
//
//	Package -[:HAS_VERSION]-> PackageVersion -[:DECLARES_DEPENDENCY]->
//	  PackageDependency -[:DEPENDS_ON_PACKAGE]-> Package
//
// Forward (direction="forward") answers "what does package X depend on": it
// anchors on the declaring Package and returns the declared dependency targets,
// keyed on (dependency_normalized, dependency uid).
//
// Reverse (direction="reverse") answers "who depends on package X": it anchors
// on the target Package via DEPENDS_ON_PACKAGE and walks back to the declaring
// PackageVersion. Declaring packages are not always materialized as Package
// nodes, so the reverse projection reports the dependent identity from the
// PackageVersion (package_id, name, version) rather than requiring a Package
// node join that would silently drop rows. Reverse is keyed on
// (dependent package_id, dependency uid).
//
// Both shapes filter on indexed anchors first, RETURN narrow scalar properties
// directly (no computed WITH re-projection, which NornicDB Bolt returns as
// literal alias text), order deterministically, apply a two-part keyset cursor,
// and LIMIT last so the LIMIT happens after the intended narrowing.
func dependenciesCypher(
	direction string,
	ecosystem string,
	pkg string,
	afterName string,
	afterEdge string,
	limit int,
) (string, map[string]any) {
	params := map[string]any{
		"after_name": afterName,
		"after_edge": afterEdge,
		"ecosystem":  ecosystem,
		"limit":      limit,
		"package":    pkg,
	}
	if direction == dependencyDirectionReverse {
		return reverseDependenciesCypher(pkg), params
	}
	return forwardDependenciesCypher(pkg), params
}

// forwardDependenciesCypher returns the "deps of an anchor" traversal. When a
// package anchor is provided the match starts from that indexed Package; the
// unanchored browse starts from every declaring Package but still filters on
// indexed anchors, projects narrow columns, and applies the keyset cursor
// before LIMIT.
func forwardDependenciesCypher(pkg string) string {
	srcAnchor := "(src:Package)"
	if pkg != "" {
		srcAnchor = "(src:Package {normalized_name: $package})"
	}
	return "MATCH " + srcAnchor + `-[:HAS_VERSION]->(v:PackageVersion)-[:DECLARES_DEPENDENCY]->(d:PackageDependency)-[:DEPENDS_ON_PACKAGE]->(target:Package)
WHERE ($ecosystem = '' OR src.ecosystem = $ecosystem)
  AND d.uid IS NOT NULL AND d.uid <> ''
  AND target.uid IS NOT NULL AND target.uid <> ''
  AND d.dependency_normalized IS NOT NULL AND d.dependency_normalized <> ''
  AND ($after_name = '' OR d.dependency_normalized > $after_name OR (d.dependency_normalized = $after_name AND d.uid > $after_edge))
RETURN 'forward' AS direction,
       src.uid AS anchor_package_id,
       src.normalized_name AS anchor_package,
       src.ecosystem AS anchor_ecosystem,
       v.version AS declaring_version,
       target.uid AS related_package_id,
       d.dependency_normalized AS related_package,
       d.dependency_ecosystem AS related_ecosystem,
       d.dependency_range AS dependency_range,
       d.dependency_type AS dependency_type,
       coalesce(d.optional, false) AS optional,
       d.uid AS edge_id,
       d.dependency_normalized AS cursor_name
ORDER BY d.dependency_normalized, d.uid
LIMIT $limit`
}

// reverseDependenciesCypher returns the "dependents of a package" traversal. It
// anchors on the target Package (required for reverse) and walks back to the
// declaring PackageVersion, reporting the dependent identity from the version
// because declaring packages may not be Package nodes.
func reverseDependenciesCypher(pkg string) string {
	targetAnchor := "(target:Package {normalized_name: $package})"
	if pkg == "" {
		targetAnchor = "(target:Package)"
	}
	return "MATCH " + targetAnchor + `<-[:DEPENDS_ON_PACKAGE]-(d:PackageDependency)<-[:DECLARES_DEPENDENCY]-(v:PackageVersion)
WHERE ($ecosystem = '' OR target.ecosystem = $ecosystem)
  AND d.uid IS NOT NULL AND d.uid <> ''
  AND target.uid IS NOT NULL AND target.uid <> ''
  AND v.package_id IS NOT NULL AND v.package_id <> ''
  AND ($after_name = '' OR v.package_id > $after_name OR (v.package_id = $after_name AND d.uid > $after_edge))
RETURN 'reverse' AS direction,
       target.uid AS anchor_package_id,
       target.normalized_name AS anchor_package,
       target.ecosystem AS anchor_ecosystem,
       v.version AS declaring_version,
       v.package_id AS related_package_id,
       v.name AS related_package,
       v.ecosystem AS related_ecosystem,
       d.dependency_range AS dependency_range,
       d.dependency_type AS dependency_type,
       coalesce(d.optional, false) AS optional,
       d.uid AS edge_id,
       v.package_id AS cursor_name
ORDER BY v.package_id, d.uid
LIMIT $limit`
}
