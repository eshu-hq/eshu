// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func packageRegistryPackagesCypher(packageID, ecosystem, name string, limit int) (string, map[string]any) {
	params := map[string]any{"limit": limit}
	var match string
	switch {
	case packageID != "":
		match = "MATCH (p:Package {uid: $package_id})"
		params["package_id"] = packageID
	case name != "":
		match = "MATCH (p:Package {ecosystem: $ecosystem, normalized_name: $name})"
		params["ecosystem"] = ecosystem
		params["name"] = name
	default:
		match = "MATCH (p:Package {ecosystem: $ecosystem})"
		params["ecosystem"] = ecosystem
	}
	return match + `
RETURN p.uid AS package_id,
       p.ecosystem AS ecosystem,
       p.registry AS registry,
       p.namespace AS namespace,
       p.normalized_name AS normalized_name,
       p.purl AS purl,
       p.bom_ref AS bom_ref,
       p.package_manager AS package_manager,
       p.source_path AS source_path,
       p.source_specific_id AS source_specific_id,
       p.visibility AS visibility,
       p.source_confidence AS source_confidence
ORDER BY p.ecosystem, p.normalized_name, p.uid
LIMIT $limit`, params
}

// packageRegistryVersionCountsCypher resolves HAS_VERSION counts for an
// explicit page of package uids as its own single-clause, MATCH-only
// statement (a concrete relationship variable anchored on bound package
// identities via UNWIND). NornicDB's OPTIONAL MATCH + count(v) aggregate
// silently collapses every zero-match group into a single row instead of
// grouping by every non-aggregate RETURN key, so packageRegistryPackagesCypher
// must not carry the version count itself. Any package uid absent from this
// query's result has zero versions; the caller zero-fills it. See
// docs/public/reference/nornicdb-pitfalls.md.
func packageRegistryVersionCountsCypher(packageIDs []string) (string, map[string]any) {
	return `UNWIND $package_ids AS candidate_package_id
MATCH (p:Package {uid: candidate_package_id})-[r:HAS_VERSION]->(v:PackageVersion)
RETURN p.uid AS package_id, count(r) AS version_count`, map[string]any{"package_ids": packageIDs}
}

func packageRegistryVersionsCypher() string {
	return `MATCH (p:Package {uid: $package_id})-[:HAS_VERSION]->(v:PackageVersion)
RETURN v.uid AS version_id,
       v.package_id AS package_id,
       v.version AS version,
       v.purl AS purl,
       v.bom_ref AS bom_ref,
       v.package_manager AS package_manager,
       v.published_at AS published_at,
       coalesce(v.is_yanked, false) AS is_yanked,
       coalesce(v.is_unlisted, false) AS is_unlisted,
       coalesce(v.is_deprecated, false) AS is_deprecated,
       coalesce(v.is_retracted, false) AS is_retracted
ORDER BY v.version, v.uid
LIMIT $limit`
}

func packageRegistryDependenciesCypher(
	packageID,
	versionID,
	afterVersionID,
	afterDependencyID string,
	limit int,
) (string, map[string]any) {
	params := map[string]any{
		"after_dependency_id": afterDependencyID,
		"after_version_id":    afterVersionID,
		"limit":               limit,
		"package_id":          packageID,
		"version_id":          versionID,
	}
	var match string
	switch {
	case versionID != "":
		match = `MATCH (d:PackageDependency)
WHERE d.version_id = $version_id`
	default:
		match = `MATCH (d:PackageDependency)
WHERE d.package_id = $package_id`
	}
	return match + `
WITH d
MATCH (d)-[:DEPENDS_ON_PACKAGE]->(target:Package)
WHERE d.uid IS NOT NULL AND d.uid <> ''
  AND d.package_id IS NOT NULL AND d.package_id <> ''
  AND d.version_id IS NOT NULL AND d.version_id <> ''
  AND target.uid IS NOT NULL AND target.uid <> ''
  AND ($package_id = '' OR d.package_id = $package_id)
  AND ($version_id = '' OR d.version_id = $version_id)
  AND ($after_version_id = '' OR d.version_id > $after_version_id OR (d.version_id = $after_version_id AND d.uid > $after_dependency_id))
RETURN d.uid AS dependency_id,
       d.package_id AS source_package_id,
       d.version_id AS source_version_id,
       d.version AS version,
       target.uid AS dependency_package_id,
       d.dependency_ecosystem AS dependency_ecosystem,
       d.dependency_registry AS dependency_registry,
       d.dependency_namespace AS dependency_namespace,
       d.dependency_normalized AS dependency_normalized,
       d.dependency_purl AS dependency_purl,
       d.dependency_bom_ref AS dependency_bom_ref,
       d.dependency_manager AS dependency_manager,
       d.dependency_range AS dependency_range,
       d.dependency_type AS dependency_type,
       d.target_framework AS target_framework,
       d.marker AS marker,
       coalesce(d.optional, false) AS optional,
       coalesce(d.excluded, false) AS excluded,
       d.source_confidence AS source_confidence,
       d.collector_kind AS collector_kind,
       d.collector_instance_id AS collector_instance_id,
       d.correlation_anchors AS correlation_anchors
ORDER BY d.version_id, d.uid
LIMIT $limit`, params
}
