// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

const readinessAffectedPackageTargetQuery = `
SELECT DISTINCT
    COALESCE(fact.payload->>'package_id', ''),
    COALESCE(fact.payload->>'purl', ''),
    COALESCE(fact.payload->>'ecosystem', ''),
    COALESCE(fact.payload->>'package_name', '')
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'vulnerability.affected_package'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'cve_id' = $1
`

const readinessDigestComponentTargetQuery = `
SELECT DISTINCT
    COALESCE(component.payload->>'package_id', ''),
    COALESCE(component.payload->>'purl', ''),
    COALESCE(component.payload->>'ecosystem', ''),
    COALESCE(component.payload->>'name', '')
FROM fact_records AS document
JOIN ingestion_scopes AS document_scope
  ON document_scope.scope_id = document.scope_id
 AND document_scope.active_generation_id = document.generation_id
JOIN scope_generations AS document_generation
  ON document_generation.scope_id = document.scope_id
 AND document_generation.generation_id = document.generation_id
JOIN fact_records AS component
  ON component.scope_id = document.scope_id
 AND component.generation_id = document.generation_id
 AND component.payload->>'document_id' = document.payload->>'document_id'
WHERE document.fact_kind = 'sbom.document'
  AND document.is_tombstone = FALSE
  AND document_generation.status = 'active'
  AND document.payload->>'subject_digest' = ANY($1::text[])
  AND component.fact_kind = 'sbom.component'
  AND component.is_tombstone = FALSE
`

const readinessImageRefDigestTargetQuery = `
SELECT DISTINCT digest
FROM container_image_identity_current_supports
WHERE image_ref = $1
  AND NULLIF(TRIM(digest), '') IS NOT NULL
`

const readinessPackageManifestConsumptionKeysReadyQuery = `
SELECT EXISTS (
    SELECT 1
    FROM package_manifest_consumption_key_backfill_markers
    WHERE marker_name = $1
) AND NOT EXISTS (
    SELECT 1 FROM package_manifest_consumption_key_dirty_scopes
)
`

type readinessTarget struct {
	PackageIDs []string
	Keys       []readinessTargetKey
}

type readinessTargetPackage struct {
	packageID string
	keys      []readinessTargetKey
}

type readinessTargetKey struct {
	packageID     string
	key           packageidentity.ConsumptionKey
	parserTrusted bool
}

func packageManifestConsumptionKeysReady(ctx context.Context, database db.Queryer) (bool, error) {
	rows, err := database.QueryContext(ctx, readinessPackageManifestConsumptionKeysReadyQuery, storagepostgres.PackageManifestConsumptionKeyBackfillMarker)
	if err != nil {
		return false, fmt.Errorf("check package manifest consumption key readiness: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var ready bool
	if err := rows.Scan(&ready); err != nil {
		return false, fmt.Errorf("scan package manifest consumption key readiness: %w", err)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("check package manifest consumption key readiness: %w", err)
	}
	return ready, nil
}

func readinessTargetArguments(target readinessTarget, query ReadinessQuery) ([]string, []string, []string, bool) {
	ecosystems := make([]string, 0, len(target.Keys))
	packageNames := make([]string, 0, len(target.Keys))
	for _, key := range target.Keys {
		ecosystems = append(ecosystems, string(key.key.Ecosystem))
		packageNames = append(packageNames, key.key.PackageName)
	}
	return ecosystems, packageNames, target.PackageIDs, query.needsTargetResolution()
}

func readinessTargetOwnerArguments(target readinessTarget) ([]string, []string, []string, []string, []string, []string) {
	packageIDs := make([]string, 0, len(target.Keys))
	ecosystems := make([]string, 0, len(target.Keys))
	packageNames := make([]string, 0, len(target.Keys))
	trustedPackageIDs := make([]string, 0, len(target.Keys))
	trustedEcosystems := make([]string, 0, len(target.Keys))
	trustedPackageNames := make([]string, 0, len(target.Keys))
	for _, entry := range target.Keys {
		packageIDs = append(packageIDs, entry.packageID)
		ecosystems = append(ecosystems, string(entry.key.Ecosystem))
		packageNames = append(packageNames, entry.key.PackageName)
		if entry.parserTrusted {
			trustedPackageIDs = append(trustedPackageIDs, entry.packageID)
			trustedEcosystems = append(trustedEcosystems, string(entry.key.Ecosystem))
			trustedPackageNames = append(trustedPackageNames, entry.key.PackageName)
		}
	}
	return packageIDs, ecosystems, packageNames, trustedPackageIDs, trustedEcosystems, trustedPackageNames
}

func resolveReadinessTarget(
	ctx context.Context,
	database db.Queryer,
	query ReadinessQuery,
) (readinessTarget, error) {
	sets := make([]map[string]readinessTargetPackage, 0, 4)
	if packageID := strings.TrimSpace(query.PackageID); packageID != "" {
		resolved, err := resolveReadinessPackageID(ctx, database, packageID)
		if err != nil {
			return readinessTarget{}, err
		}
		sets = append(sets, map[string]readinessTargetPackage{resolved.packageID: resolved})
	}
	if cveID := strings.TrimSpace(query.CVEID); cveID != "" {
		resolved, err := resolveReadinessTargetPackages(ctx, database, readinessAffectedPackageTargetQuery, cveID)
		if err != nil {
			return readinessTarget{}, fmt.Errorf("resolve CVE affected packages: %w", err)
		}
		sets = append(sets, resolved)
	}
	digests, err := resolveReadinessImageDigests(ctx, database, query)
	if err != nil {
		return readinessTarget{}, err
	}
	if len(digests) > 0 || strings.TrimSpace(query.SubjectDigest) != "" || strings.TrimSpace(query.ImageRef) != "" {
		resolved, err := resolveReadinessTargetPackages(ctx, database, readinessDigestComponentTargetQuery, array.Of(digests))
		if err != nil {
			return readinessTarget{}, fmt.Errorf("resolve digest SBOM packages: %w", err)
		}
		sets = append(sets, resolved)
	}
	return intersectReadinessTargetPackages(sets), nil
}

func resolveReadinessImageDigests(ctx context.Context, database db.Queryer, query ReadinessQuery) ([]string, error) {
	directDigest := strings.TrimSpace(query.SubjectDigest)
	imageRef := strings.TrimSpace(query.ImageRef)
	if imageRef == "" {
		if directDigest == "" {
			return nil, nil
		}
		return []string{directDigest}, nil
	}
	rows, err := database.QueryContext(ctx, readinessImageRefDigestTargetQuery, imageRef)
	if err != nil {
		return nil, fmt.Errorf("resolve image reference digests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	matched := make(map[string]struct{})
	for rows.Next() {
		var digest string
		if err := rows.Scan(&digest); err != nil {
			return nil, fmt.Errorf("scan image reference digest: %w", err)
		}
		if digest = strings.TrimSpace(digest); digest != "" && (directDigest == "" || digest == directDigest) {
			matched[digest] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve image reference digests: %w", err)
	}
	result := make([]string, 0, len(matched))
	for digest := range matched {
		result = append(result, digest)
	}
	sort.Strings(result)
	return result, nil
}

func resolveReadinessTargetPackages(
	ctx context.Context,
	database db.Queryer,
	statement string, args ...any,
) (map[string]readinessTargetPackage, error) {
	rows, err := database.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	type sourcePackage struct {
		packageID   string
		purl        string
		ecosystem   string
		packageName string
	}
	var sources []sourcePackage
	for rows.Next() {
		var source sourcePackage
		if err := rows.Scan(&source.packageID, &source.purl, &source.ecosystem, &source.packageName); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan target package: %w", err)
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close target package rows: %w", err)
	}

	packageIDSet := make(map[string]struct{})
	for _, source := range sources {
		if packageID := strings.TrimSpace(source.packageID); packageID != "" && strings.TrimSpace(source.purl) == "" {
			packageIDSet[packageID] = struct{}{}
		}
	}
	packageIDs := make([]string, 0, len(packageIDSet))
	for packageID := range packageIDSet {
		packageIDs = append(packageIDs, packageID)
	}
	sort.Strings(packageIDs)
	registryKeys, err := loadReadinessRegistryKeysForPackageIDs(ctx, database, packageIDs)
	if err != nil {
		return nil, err
	}
	resolved := make(map[string]readinessTargetPackage, len(sources))
	for _, source := range sources {
		entry, err := resolveReadinessTargetPackage(source.packageID, source.purl, source.ecosystem, source.packageName, registryKeys)
		if err != nil {
			return nil, err
		}
		resolved[entry.packageID] = mergeReadinessTargetPackage(resolved[entry.packageID], entry)
	}
	return resolved, nil
}

func resolveReadinessTargetPackage(
	packageID, purl, ecosystem, packageName string,
	registryKeys map[string][]packageidentity.ConsumptionKey,
) (readinessTargetPackage, error) {
	packageID = strings.TrimSpace(packageID)
	purl = strings.TrimSpace(purl)
	if purl != "" {
		purlPackageID, err := packageidentity.PackageIDFromPURL(purl)
		if err != nil {
			return readinessTargetPackage{}, fmt.Errorf("parse package PURL: %w", err)
		}
		if packageID != "" && packageID != purlPackageID {
			return readinessTargetPackage{}, fmt.Errorf("package ID %q disagrees with PURL identity %q", packageID, purlPackageID)
		}
		keys := packageidentity.ConsumptionKeysFromPURL(purl)
		if len(keys) == 0 {
			return readinessTargetPackage{}, fmt.Errorf("PURL %q has no supported manifest-consumption identity", purl)
		}
		return readinessTargetPackageFromKeys(purlPackageID, keys, true), nil
	}
	if packageID == "" && strings.TrimSpace(ecosystem) != "" && strings.TrimSpace(packageName) != "" {
		normalizedEcosystem := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(ecosystem))
		identity, err := packageidentity.Normalize(packageidentity.RawIdentity{
			Ecosystem: normalizedEcosystem,
			Registry:  packageidentity.DefaultRegistry(normalizedEcosystem),
			RawName:   packageName,
		})
		if err != nil {
			return readinessTargetPackage{}, fmt.Errorf("parse default-registry target identity: %w", err)
		}
		keys := packageidentity.ConsumptionKeys(string(normalizedEcosystem), packageName)
		if len(keys) == 0 {
			return readinessTargetPackage{}, fmt.Errorf("target package %q has no supported manifest-consumption identity", packageName)
		}
		return readinessTargetPackageFromKeys(identity.PackageID, keys, true), nil
	}
	return readinessTargetPackageForPackageID(packageID, registryKeys[packageID])
}

func intersectReadinessTargetPackages(sets []map[string]readinessTargetPackage) readinessTarget {
	if len(sets) == 0 {
		return readinessTarget{}
	}
	intersection := make(map[string]readinessTargetPackage, len(sets[0]))
	for packageID, entry := range sets[0] {
		intersection[packageID] = entry
	}
	for _, candidates := range sets[1:] {
		for packageID := range intersection {
			if _, ok := candidates[packageID]; !ok {
				delete(intersection, packageID)
			}
		}
	}
	packageIDs := make([]string, 0, len(intersection))
	var keys []readinessTargetKey
	for packageID := range intersection {
		packageIDs = append(packageIDs, packageID)
		for _, candidates := range sets {
			keys = append(keys, candidates[packageID].keys...)
		}
	}
	sort.Strings(packageIDs)
	return readinessTarget{PackageIDs: packageIDs, Keys: uniqueReadinessTargetKeys(keys)}
}

func mergeReadinessTargetPackage(existing, candidate readinessTargetPackage) readinessTargetPackage {
	if existing.packageID == "" {
		return candidate
	}
	existing.keys = uniqueReadinessTargetKeys(append(existing.keys, candidate.keys...))
	return existing
}

func readinessTargetPackageFromKeys(packageID string, keys []packageidentity.ConsumptionKey, parserTrusted bool) readinessTargetPackage {
	entries := make([]readinessTargetKey, 0, len(keys))
	for _, key := range keys {
		if key.Ecosystem != "" && key.PackageName != "" {
			entries = append(entries, readinessTargetKey{packageID: packageID, key: key, parserTrusted: parserTrusted})
		}
	}
	return readinessTargetPackage{packageID: packageID, keys: uniqueReadinessTargetKeys(entries)}
}

func uniqueReadinessTargetKeys(keys []readinessTargetKey) []readinessTargetKey {
	byIdentity := make(map[readinessTargetKey]bool, len(keys))
	for _, entry := range keys {
		if entry.packageID == "" || entry.key.Ecosystem == "" || entry.key.PackageName == "" {
			continue
		}
		identity := entry
		identity.parserTrusted = false
		byIdentity[identity] = byIdentity[identity] || entry.parserTrusted
	}
	result := make([]readinessTargetKey, 0, len(byIdentity))
	for identity, parserTrusted := range byIdentity {
		identity.parserTrusted = parserTrusted
		result = append(result, identity)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].packageID != result[j].packageID {
			return result[i].packageID < result[j].packageID
		}
		if result[i].key.Ecosystem != result[j].key.Ecosystem {
			return result[i].key.Ecosystem < result[j].key.Ecosystem
		}
		return result[i].key.PackageName < result[j].key.PackageName
	})
	return result
}
