// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func listOSPackageAdvisoryTargetsQuery() string {
	return `
WITH active_os_packages AS (
  SELECT
    LOWER(COALESCE(NULLIF(fact.payload->>'vendor_advisory_source', ''), fact.payload->>'distro')) AS ecosystem,
    COALESCE(fact.payload->>'distro', '') AS distro,
    COALESCE(fact.payload->>'distro_version', '') AS distro_version,
    COALESCE(fact.payload->>'name', '') AS package_name,
    COALESCE(fact.payload->>'installed_version_raw', '') AS installed_version,
    COALESCE(fact.payload->>'package_manager', '') AS package_manager,
    COALESCE(fact.payload->>'arch', '') AS arch,
    COALESCE(fact.payload->>'vendor_advisory_source', '') AS vendor_advisory_source,
    COALESCE(fact.payload->>'repository_class', '') AS repository_class,
    COALESCE(fact.payload->>'purl', '') AS purl,
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    ROW_NUMBER() OVER (ORDER BY fact.fact_id ASC) - 1 AS target_rank,
    COUNT(*) OVER () AS total_targets
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
  WHERE fact.fact_kind = 'vulnerability.os_package'
    AND fact.is_tombstone = FALSE
    AND generation.status = 'active'
    AND LOWER(COALESCE(NULLIF(fact.payload->>'vendor_advisory_source', ''), fact.payload->>'distro')) = ANY($1::text[])
),
rotated_targets AS (
  SELECT *,
    MOD(target_rank - MOD($3::bigint, total_targets) + total_targets, total_targets) AS rotated_rank
  FROM active_os_packages
)
SELECT
  ecosystem,
  distro,
  distro_version,
  package_name,
  installed_version,
  package_manager,
  arch,
  vendor_advisory_source,
  repository_class,
  purl,
  fact_id,
  scope_id,
  generation_id
FROM rotated_targets
ORDER BY rotated_rank ASC, target_rank ASC
LIMIT $2
`
}

func listSBOMComponentAdvisoryTargetsQuery() string {
	return `
WITH active_components AS (
  SELECT
    CASE LOWER(SPLIT_PART(SUBSTRING(COALESCE(component.payload->>'purl', '') FROM 5), '/', 1))
      WHEN 'golang' THEN 'go'
      WHEN 'gem' THEN 'rubygems'
      ELSE LOWER(SPLIT_PART(SUBSTRING(COALESCE(component.payload->>'purl', '') FROM 5), '/', 1))
    END AS ecosystem,
    COALESCE(component.payload->>'purl', '') AS purl,
    COALESCE(component.payload->>'name', '') AS package_name,
    COALESCE(component.payload->>'version', '') AS version,
    COALESCE(component.payload->>'document_id', '') AS document_id,
    COALESCE(attachment.payload->>'subject_digest', '') AS subject_digest,
    component.fact_id,
    ROW_NUMBER() OVER (ORDER BY component.fact_id ASC) - 1 AS target_rank
  FROM fact_records AS component
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = component.scope_id
   AND scope.active_generation_id = component.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = component.scope_id
   AND generation.generation_id = component.generation_id
  JOIN fact_records AS attachment
    ON attachment.fact_kind = 'reducer_sbom_attestation_attachment'
   AND attachment.is_tombstone = FALSE
   AND attachment.scope_id = component.scope_id
   AND component.payload->>'document_id' = attachment.payload->>'document_id'
   AND attachment.payload->>'attachment_status' IN ('attached_verified', 'attached_unverified', 'attached_parse_only')
  JOIN ingestion_scopes AS attachment_scope
    ON attachment_scope.scope_id = attachment.scope_id
   AND attachment_scope.active_generation_id = attachment.generation_id
  JOIN scope_generations AS attachment_generation
    ON attachment_generation.scope_id = attachment.scope_id
   AND attachment_generation.generation_id = attachment.generation_id
  WHERE component.fact_kind = 'sbom.component'
    AND component.is_tombstone = FALSE
    AND generation.status = 'active'
    AND attachment_generation.status = 'active'
),
numbered_targets AS (
  SELECT
    *,
    COUNT(*) OVER () AS total_targets
  FROM active_components
  WHERE ecosystem = ANY($1::text[])
),
rotated_targets AS (
  SELECT *,
    MOD(target_rank - MOD($3::bigint, total_targets) + total_targets, total_targets) AS rotated_rank
  FROM numbered_targets
)
SELECT
  purl,
  package_name,
  version,
  document_id,
  subject_digest,
  fact_id
FROM rotated_targets
ORDER BY rotated_rank ASC, target_rank ASC
LIMIT $2
`
}

// ListOSPackageAdvisoryTargets loads active installed OS package evidence for
// vulnerability-intelligence advisory target derivation.
func (s FactStore) ListOSPackageAdvisoryTargets(
	ctx context.Context,
	filter workflow.OSPackageAdvisoryTargetFilter,
) ([]workflow.OSPackageAdvisoryTarget, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	ecosystems := cleanStringFilterValues(filter.Ecosystems)
	if len(ecosystems) == 0 {
		return nil, nil
	}
	limit := ownedPackageDependencyTargetLimit(filter.Limit)
	rows, err := s.database.QueryContext(ctx, listOSPackageAdvisoryTargetsQuery(), ecosystems, limit, filter.RotationOffset)
	if err != nil {
		return nil, fmt.Errorf("list OS package advisory targets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	targets := make([]workflow.OSPackageAdvisoryTarget, 0, limit)
	for rows.Next() {
		var target workflow.OSPackageAdvisoryTarget
		if err := rows.Scan(
			&target.Ecosystem,
			&target.Distro,
			&target.DistroVersion,
			&target.PackageName,
			&target.InstalledVersion,
			&target.PackageManager,
			&target.Arch,
			&target.VendorAdvisorySource,
			&target.RepositoryClass,
			&target.PURL,
			&target.FactID,
			&target.ScopeID,
			&target.GenerationID,
		); err != nil {
			return nil, fmt.Errorf("list OS package advisory targets: %w", err)
		}
		normalizeOSPackageAdvisoryTarget(&target)
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list OS package advisory targets: %w", err)
	}
	return targets, nil
}

// osPackagePURLTrimSet is the character set btrim strips in
// osPackagePURLPrefixExpression: exactly the runes Go's strings.TrimSpace
// strips (unicode.IsSpace: 25 runes), written as escapes so the text is
// identical in the query and in migration 153. Postgres btrim(string, chars)
// trims any character in chars from both ends, so this reproduces
// TrimSpace, and TestOSPackagePURLTrimSetMatchesGoTrimSpace enumerates the
// runes to prove it. VT is written as \u000b because Postgres escape strings
// do not define \v.
const osPackagePURLTrimSet = `E' \t\n\u000b\f\r\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000'`

// osPackagePURLPrefixExpression is the SQL form of the installed package id
// the reducer matches on: core.packageIDFromPURL, which is strings.TrimSpace of
// the purl cut at its first '@' (go/internal/reducer/supplychain/core/
// purl_match.go). payloadColumn names the jsonb payload column, with or
// without a table alias. Migration 153 builds an expression index on exactly
// this text with payload unqualified, and TestOSPackagePURLPrefixExpression
// MatchesMigration pins the two together: an index on any other spelling would
// not serve the query.
func osPackagePURLPrefixExpression(payloadColumn string) string {
	return "split_part(btrim(" + payloadColumn + "->>'purl', " + osPackagePURLTrimSet + "), '@', 1)"
}

// listOSPackageAdvisoryTargetsForPackagesQuery is one keyset page of installed
// vulnerability.os_package targets whose package id is one of $1 (#7154).
//
// The finding set of an impact intent depends only on installed rows whose
// package id equals one of the intent's affected-package lookup keys, so
// narrowing the read to those keys is complete over a small closed set, where
// the ecosystem-only read it replaces drained every installed package of the
// ecosystem across the whole fleet for every intent. The candidates CTE is
// MATERIALIZED and driven by fact_records_os_package_purl_prefix_idx: without
// the CTE the planner drove from the scope join, misestimated it, and read every
// row on every page even with the index present.
//
// Every candidate row advances the cursor and counts toward the page, whether or
// not its generation is active, so a page of inactive rows cannot end a drain
// early; the active flag (an active scope generation and an ecosystem in $4,
// the same predicate the rotation reader applies) is applied by the caller. A
// page shorter than $3 candidates is the last one. ORDER BY and the `>` cursor
// use the same fact_id column and collation, so they agree.
//
// An empty $2 starts from the first target: every fact id sorts after the
// empty string.
func listOSPackageAdvisoryTargetsForPackagesQuery() string {
	return `
WITH candidates AS MATERIALIZED (
  SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.payload
  FROM fact_records AS fact
  WHERE fact.fact_kind = 'vulnerability.os_package'
    AND fact.is_tombstone = FALSE
    AND ` + osPackagePURLPrefixExpression("fact.payload") + ` = ANY($1::text[])
    AND fact.fact_id > $2
  ORDER BY fact.fact_id ASC
  LIMIT $3
)
SELECT
  LOWER(COALESCE(NULLIF(c.payload->>'vendor_advisory_source', ''), c.payload->>'distro')) AS ecosystem,
  COALESCE(c.payload->>'distro', '') AS distro,
  COALESCE(c.payload->>'distro_version', '') AS distro_version,
  COALESCE(c.payload->>'name', '') AS package_name,
  COALESCE(c.payload->>'installed_version_raw', '') AS installed_version,
  COALESCE(c.payload->>'package_manager', '') AS package_manager,
  COALESCE(c.payload->>'arch', '') AS arch,
  COALESCE(c.payload->>'vendor_advisory_source', '') AS vendor_advisory_source,
  COALESCE(c.payload->>'repository_class', '') AS repository_class,
  COALESCE(c.payload->>'purl', '') AS purl,
  c.fact_id,
  c.scope_id,
  c.generation_id,
  (scope.scope_id IS NOT NULL
    AND COALESCE(generation.status, '') = 'active'
    AND LOWER(COALESCE(NULLIF(c.payload->>'vendor_advisory_source', ''), c.payload->>'distro')) = ANY($4::text[])) AS active
FROM candidates AS c
LEFT JOIN ingestion_scopes AS scope
  ON scope.scope_id = c.scope_id
 AND scope.active_generation_id = c.generation_id
LEFT JOIN scope_generations AS generation
  ON generation.scope_id = c.scope_id
 AND generation.generation_id = c.generation_id
ORDER BY c.fact_id ASC
`
}

// normalizeOSPackageAdvisoryTarget applies the trimming and case folding the
// rotation reader applies to every scanned target.
func normalizeOSPackageAdvisoryTarget(target *workflow.OSPackageAdvisoryTarget) {
	target.Ecosystem = strings.ToLower(strings.TrimSpace(target.Ecosystem))
	target.Distro = strings.ToLower(strings.TrimSpace(target.Distro))
	target.DistroVersion = strings.TrimSpace(target.DistroVersion)
	target.PackageName = strings.TrimSpace(target.PackageName)
	target.InstalledVersion = strings.TrimSpace(target.InstalledVersion)
	target.PackageManager = strings.ToLower(strings.TrimSpace(target.PackageManager))
	target.Arch = strings.TrimSpace(target.Arch)
	target.VendorAdvisorySource = strings.ToLower(strings.TrimSpace(target.VendorAdvisorySource))
	target.RepositoryClass = strings.ToLower(strings.TrimSpace(target.RepositoryClass))
	target.ScopeID = strings.TrimSpace(target.ScopeID)
	target.GenerationID = strings.TrimSpace(target.GenerationID)
}

// ListSBOMComponentAdvisoryTargets loads active attached SBOM component
// evidence for vulnerability-intelligence advisory target derivation.
func (s FactStore) ListSBOMComponentAdvisoryTargets(
	ctx context.Context,
	filter workflow.SBOMComponentAdvisoryTargetFilter,
) ([]workflow.SBOMComponentAdvisoryTarget, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	ecosystems := cleanStringFilterValues(filter.Ecosystems)
	if len(ecosystems) == 0 {
		return nil, nil
	}
	limit := ownedPackageDependencyTargetLimit(filter.Limit)
	rows, err := s.database.QueryContext(ctx, listSBOMComponentAdvisoryTargetsQuery(), ecosystems, limit, filter.RotationOffset)
	if err != nil {
		return nil, fmt.Errorf("list SBOM component advisory targets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	targets := make([]workflow.SBOMComponentAdvisoryTarget, 0, limit)
	for rows.Next() {
		var row sbomComponentAdvisoryTargetRow
		if err := rows.Scan(
			&row.PURL,
			&row.PackageName,
			&row.Version,
			&row.DocumentID,
			&row.SubjectDigest,
			&row.FactID,
		); err != nil {
			return nil, fmt.Errorf("list SBOM component advisory targets: %w", err)
		}
		target, ok := sbomComponentAdvisoryTargetFromRow(row)
		if ok {
			targets = append(targets, target)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list SBOM component advisory targets: %w", err)
	}
	return targets, nil
}

type sbomComponentAdvisoryTargetRow struct {
	PURL          string
	PackageName   string
	Version       string
	DocumentID    string
	SubjectDigest string
	FactID        string
}

func sbomComponentAdvisoryTargetFromRow(row sbomComponentAdvisoryTargetRow) (workflow.SBOMComponentAdvisoryTarget, bool) {
	identity := installedAdvisoryPURLIdentity(row.PURL)
	if identity.ecosystem == "" || identity.packageName == "" || identity.version == "" {
		return workflow.SBOMComponentAdvisoryTarget{}, false
	}
	if conflictingSBOMComponentVersion(row.Version, identity.version) {
		return workflow.SBOMComponentAdvisoryTarget{}, false
	}
	target := workflow.SBOMComponentAdvisoryTarget{
		Ecosystem:     identity.ecosystem,
		PackageName:   identity.packageName,
		Version:       identity.version,
		PURL:          strings.TrimSpace(row.PURL),
		DocumentID:    strings.TrimSpace(row.DocumentID),
		SubjectDigest: strings.TrimSpace(row.SubjectDigest),
		FactID:        strings.TrimSpace(row.FactID),
	}
	return target, true
}

func conflictingSBOMComponentVersion(observed string, purlVersion string) bool {
	observed = strings.TrimSpace(observed)
	purlVersion = strings.TrimSpace(purlVersion)
	return observed != "" && purlVersion != "" && observed != purlVersion
}

type installedAdvisoryPURLParts struct {
	ecosystem   string
	packageName string
	version     string
}

func installedAdvisoryPURLIdentity(raw string) installedAdvisoryPURLParts {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "pkg:") {
		return installedAdvisoryPURLParts{}
	}
	rest := strings.TrimPrefix(trimmed, "pkg:")
	if before, _, ok := strings.Cut(rest, "?"); ok {
		rest = before
	}
	ecosystem, path, ok := strings.Cut(rest, "/")
	if !ok {
		return installedAdvisoryPURLParts{}
	}
	packagePath := path
	version := ""
	if before, after, ok := strings.Cut(path, "@"); ok {
		packagePath = before
		version = urlDecodePath(after)
	}
	return installedAdvisoryPURLParts{
		ecosystem:   installedAdvisoryPURLTypeEcosystem(ecosystem),
		packageName: urlDecodePath(strings.Trim(packagePath, "/")),
		version:     strings.TrimSpace(version),
	}
}

func installedAdvisoryPURLTypeEcosystem(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "golang":
		return "go"
	case "gem":
		return "rubygems"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func urlDecodePath(raw string) string {
	decoded, err := url.PathUnescape(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimSpace(raw)
	}
	return strings.TrimSpace(decoded)
}
