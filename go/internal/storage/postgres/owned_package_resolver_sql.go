// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

// ownedResolverParts are the pieces of the production owned-package reader and
// manifest reader the resolver statements are assembled from. Every piece is
// sliced out of the shipped query constants at a stable boundary marker, never
// copied, so a change to the reader's row selection, DISTINCT ON identity,
// slot key or folded-name predicate reaches the resolver in the same build.
// TestOwnedResolverPartsAreSlicesOfProduction pins the slicing, and the live
// differential pins the behavior.
type ownedResolverParts struct {
	// activeDependencies is the body of the reader's active_dependencies CTE:
	// the active-generation, non-tombstone, package_manager = ANY($1) read with
	// the alias-expanded ecosystem column. It ends with a newline so a
	// predicate can be appended.
	activeDependencies string
	// distinctColumns and distinctOrder are the reader's DISTINCT ON identity
	// (ecosystem, package_name, version) and its tie-break order.
	distinctColumns string
	distinctOrder   string
	// slotKey is the SQL text the reader hashes: ecosystem|package_name|version
	// over the stored, raw package_name.
	slotKey string
	// low24 is the reader's expression for the low 24 bits of that hash.
	low24 string
	// foldedName is the exact expression of the folded entity index
	// (migration 247) as the manifest reader spells it.
	foldedName string
}

var ownedResolverPartsOnce = sync.OnceValues(buildOwnedResolverParts)

// between returns the text of s strictly between the first start marker and
// the first end marker after it.
func between(s, start, end string) (string, error) {
	_, after, ok := strings.Cut(s, start)
	if !ok {
		return "", fmt.Errorf("owned resolver: start marker %q not found in the reader statement", start)
	}
	body, _, ok := strings.Cut(after, end)
	if !ok {
		return "", fmt.Errorf("owned resolver: end marker %q not found in the reader statement", end)
	}
	return body, nil
}

func buildOwnedResolverParts() (ownedResolverParts, error) {
	reader := listOwnedPackageDependencyTargetsQuery(true, true)
	var parts ownedResolverParts
	var err error
	if parts.activeDependencies, err = between(reader, "WITH active_dependencies AS (\n", "),\ndistinct_targets AS MATERIALIZED ("); err != nil {
		return ownedResolverParts{}, err
	}
	if parts.distinctColumns, err = between(reader, "SELECT DISTINCT ON (", ")\n"); err != nil {
		return ownedResolverParts{}, err
	}
	if parts.distinctOrder, err = between(reader, "FROM active_dependencies\n  ORDER BY ", "\n),\nnumbered_targets"); err != nil {
		return ownedResolverParts{}, err
	}
	if parts.slotKey, err = between(reader, "sha256(convert_to(", ", 'UTF8')) AS key_hash"); err != nil {
		return ownedResolverParts{}, err
	}
	low24Body, err := between(reader, "get_byte(key_hash, 0)", " AS key_hash_low24")
	if err != nil {
		return ownedResolverParts{}, err
	}
	parts.low24 = "get_byte(key_hash, 0)" + low24Body
	foldLine, err := between(listActivePackageManifestDependencyFactsQuery, "  AND (LOWER(TRIM(", ") = ANY($2::text[])")
	if err != nil {
		return ownedResolverParts{}, err
	}
	parts.foldedName = "(LOWER(TRIM(" + foldLine + ")"
	return parts, nil
}

// ownedResolverSelect wraps a matched-rows source in the reader's DISTINCT ON
// identity and order, hashes the slot key in SQL exactly as the reader does,
// and projects the columns the resolver needs.
func ownedResolverSelect(parts ownedResolverParts, matched string) string {
	return fmt.Sprintf(`SELECT
    picked.ecosystem,
    picked.package_name,
    picked.version,
    picked.source_location,
    picked.sbt_cross_version,
    picked.sbt_binary_artifact,
    %[1]s AS key_hash_low24
FROM (
  SELECT DISTINCT ON (%[2]s)
    matched.*,
    sha256(convert_to(%[3]s, 'UTF8')) AS key_hash
  FROM (
%[4]s
  ) AS matched
  ORDER BY %[5]s
) AS picked`, parts.low24, parts.distinctColumns, parts.slotKey, matched, parts.distinctOrder)
}

// ownedResolverByNameQuery seeks the folded entity index once per name.
// Binds: $1 raw package_manager values (the reader's bind set), $2 folded
// names. OFFSET 0 is the optimization fence that keeps the LATERAL body a
// per-name subplan: without it the planner pulls the body up into a hash join
// and reads the whole package_manager prefix of the folded index (measured on
// the QA read replica: 241,106 npm rows, 5.8 s for 40 names), the same shape a
// name list read as ANY(subquery) produces.
func ownedResolverByNameQuery(parts ownedResolverParts) string {
	matched := "    SELECT dependency.* FROM (SELECT DISTINCT lookup.name FROM unnest($2::text[]) AS lookup(name)) AS names\n" +
		"    CROSS JOIN LATERAL (\n" + parts.activeDependencies +
		"  AND " + parts.foldedName + " = names.name\n" +
		"    OFFSET 0\n" +
		"    ) AS dependency"
	return ownedResolverSelect(parts, matched)
}

// ownedResolverBinaryArtifactQuery reaches sbt "%%" rows, whose entity_name is
// the unsuffixed coordinate and whose published artifact rides in
// sbt_binary_artifact. The artifact has no folded index, so the read is the
// package_manager = 'sbt' prefix. Binds: $1 as above, $2 folded artifacts.
func ownedResolverBinaryArtifactQuery(parts ownedResolverParts) string {
	matched := "    SELECT dependency.* FROM (\n" + parts.activeDependencies +
		"  AND fact.payload->'entity_metadata'->>'package_manager' = 'sbt'\n" +
		"  AND LOWER(fact.payload->'entity_metadata'->>'sbt_binary_artifact' COLLATE \"C\") = ANY($2::text[])\n" +
		"    ) AS dependency"
	return ownedResolverSelect(parts, matched)
}

// ownedResolverPyPIQuery reads the PyPI prefix once per call and compares PEP
// 503 names on both sides, so a stored separator run ("foo__bar") and a long
// mixed name match the feed's spelling. The folded index cannot serve the
// normalized expression, so this departs from the per-name seek; it reads every
// row of the package manager's prefix, linear in PyPI rows, and only matched
// rows reach Go. Binds: $1 as above, $2 PEP 503 names.
func ownedResolverPyPIQuery(parts ownedResolverParts) string {
	matched := "    SELECT dependency.* FROM (\n" + parts.activeDependencies +
		"  AND regexp_replace(" + parts.foldedName + ", '" + packageidentity.PyPINameSeparatorRun + "', '-', 'g') = ANY($2::text[])\n" +
		"    ) AS dependency"
	return ownedResolverSelect(parts, matched)
}

// ownedResolverSwiftQuery reads every active swift dependency identity. A
// swift package is named by a repository URL held in source_location, which
// has no index and must be normalized in Go, so every identity reaches the
// resolver. The reader's own DISTINCT ON order picks the
// source_location-bearing row the planner derives the OSV name from. Bind: $1
// raw package_manager values, which select the swift prefix.
func ownedResolverSwiftQuery(parts ownedResolverParts) string {
	matched := "    SELECT dependency.* FROM (\n" + parts.activeDependencies + "    ) AS dependency"
	return ownedResolverSelect(parts, matched)
}
