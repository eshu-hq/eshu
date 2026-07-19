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
// grouping by every non-aggregate RETURN key, so neither
// packageRegistryPackagesCypher nor packageRegistryPackagesScopedEcosystemCypher
// may carry the version count itself. Any package uid absent from this
// query's result has zero versions; the caller
// (PackageRegistryHandler.attachPackageVersionCounts) zero-fills it for
// every listPackages branch, scoped and unscoped alike. See
// docs/public/reference/nornicdb-pitfalls.md.
func packageRegistryVersionCountsCypher(packageIDs []string) (string, map[string]any) {
	return `UNWIND $package_ids AS candidate_package_id
MATCH (p:Package {uid: candidate_package_id})-[r:HAS_VERSION]->(v:PackageVersion)
RETURN p.uid AS package_id, count(r) AS version_count`, map[string]any{"package_ids": packageIDs}
}

// packageRegistryPackagesScopedEcosystemCypher is the scoped-caller variant
// of the ecosystem-only browse branch of packageRegistryPackagesCypher: it
// adds a visibility='public' predicate so a scoped caller's ecosystem browse
// never returns private/unknown rows (correlation-augmented private-package
// inclusion in scoped browse is deferred; see the F-6/W5b decision doc).
//
// The ecosystem and visibility predicates are combined in ONE WHERE clause
// (`MATCH (p:Package) WHERE p.ecosystem = $ecosystem AND p.visibility =
// 'public'`) rather than keeping the ecosystem filter as an inline MATCH
// property with a trailing WHERE clause appended
// (`MATCH (p:Package {ecosystem: $ecosystem}) WHERE p.visibility =
// 'public'`). The latter shape is BROKEN on the pinned NornicDB build: it
// silently drops the inline pattern's ecosystem filter and falls back to an
// unfiltered :Package label scan, ignoring $ecosystem entirely -- verified
// against both the HTTP tx/commit endpoint and the real Bolt protocol
// (`MATCH (p:Package {ecosystem: $ecosystem}) WHERE p.visibility = 'public'
// RETURN count(p)` returned the SAME total regardless of $ecosystem's
// value). That is both a cross-ecosystem correctness leak and a latent
// full-scan performance regression. See
// docs/public/reference/nornicdb-pitfalls.md.
//
// This is deliberately an anchor-only read with NO version count column: the
// same OPTIONAL MATCH (p)-[:HAS_VERSION]->(v) ... count(v) shape that made
// packageRegistryPackagesCypher silently collapse every zero-version package
// out of its result set (see the "OPTIONAL MATCH + Aggregate Collapses Every
// Zero-Match Group Into One Row" pitfall) applies identically here -- a
// public package with zero versions would vanish from the ecosystem-browse
// page. The caller resolves version_count for the returned page via
// packageRegistryVersionCountsCypher/attachPackageVersionCounts, exactly like
// the unscoped packageRegistryPackagesCypher branches.
func packageRegistryPackagesScopedEcosystemCypher(ecosystem string, limit int) (string, map[string]any) {
	params := map[string]any{"limit": limit, "ecosystem": ecosystem}
	return `MATCH (p:Package)
WHERE p.ecosystem = $ecosystem AND p.visibility = 'public'
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

// packageRegistryAnchorVisibilityCypher resolves one Package node's
// visibility by its indexed uid (uidConstraintLabels includes "Package"), so
// the scoped-access gate can decide public-vs-gated before running the full
// anchored read. Zero rows means the package does not exist.
const packageRegistryAnchorVisibilityCypher = `MATCH (p:Package {uid: $package_id}) RETURN p.visibility AS visibility`

// packageRegistryNameAnchorVisibilityCypher resolves every Package node's uid
// and visibility matching the {ecosystem, normalized_name} anchor the
// unscoped packages-by-name branch already uses
// (packageRegistryPackagesCypher). normalized_name is not a unique identity
// within an ecosystem -- distinct registries or namespaces can share it --
// so the caller MUST gate and return every row, not just the first. Bounded
// by a generous fixed LIMIT (real collisions are a handful at most; this is
// a defense-in-depth cap, not an expected page size) and ordered by uid for
// determinism. No composite index backs this pair; it mirrors the existing
// production anchor rather than introducing a new query shape.
const packageRegistryNameAnchorVisibilityCypher = `MATCH (p:Package {ecosystem: $ecosystem, normalized_name: $name}) RETURN p.uid AS package_id, p.visibility AS visibility ORDER BY p.uid LIMIT 50`

// packageRegistryVersionAnchorPackageIDCypher resolves a PackageVersion's
// owning package id by its indexed uid (uidConstraintLabels includes
// "PackageVersion"), for the dependencies-by-version_id path that has no
// package_id anchor of its own.
const packageRegistryVersionAnchorPackageIDCypher = `MATCH (v:PackageVersion {uid: $version_id}) RETURN v.package_id AS package_id`

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
