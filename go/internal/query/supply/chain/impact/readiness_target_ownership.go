// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

const readinessTargetKeyOwnersQuery = `
WITH requested AS (
    SELECT DISTINCT package_id, ecosystem, package_name
    FROM UNNEST($1::text[], $2::text[], $3::text[]) AS target(package_id, ecosystem, package_name)
),
trusted AS (
    SELECT DISTINCT package_id, ecosystem, package_name
    FROM UNNEST($4::text[], $5::text[], $6::text[]) AS target(package_id, ecosystem, package_name)
),
ownership AS (
    SELECT
        requested.package_id,
        requested.ecosystem,
        requested.package_name,
        COUNT(DISTINCT owner.package_id) AS owner_count,
        COALESCE(BOOL_OR(owner.package_id = requested.package_id), FALSE) AS owns_requested_package,
        EXISTS (
            SELECT 1
            FROM trusted
            WHERE trusted.package_id = requested.package_id
              AND trusted.ecosystem = requested.ecosystem
              AND trusted.package_name = requested.package_name
        ) AS parser_trusted
    FROM requested
    LEFT JOIN LATERAL (
        SELECT owner.package_id
        FROM package_registry_identity_keys AS owner
        JOIN fact_records AS fact
          ON fact.fact_id = owner.fact_id
         AND fact.scope_id = owner.scope_id
         AND fact.generation_id = owner.generation_id
        JOIN ingestion_scopes AS scope
          ON scope.scope_id = owner.scope_id
         AND scope.active_generation_id = owner.generation_id
        JOIN scope_generations AS generation
          ON generation.scope_id = owner.scope_id
         AND generation.generation_id = owner.generation_id
        WHERE owner.ecosystem = requested.ecosystem
          AND owner.package_name = requested.package_name
          AND fact.fact_kind = 'package_registry.package'
          AND fact.is_tombstone = FALSE
          AND generation.status = 'active'
    ) AS owner ON TRUE
    GROUP BY requested.package_id, requested.ecosystem, requested.package_name
)
SELECT COALESCE(BOOL_AND(
    (owner_count = 1 AND owns_requested_package)
    OR (owner_count = 0 AND parser_trusted)
), TRUE)
FROM ownership
`

func validateReadinessTargetKeyOwners(ctx context.Context, database ReadinessQueryer, target readinessTarget) error {
	if len(target.Keys) == 0 {
		return nil
	}
	packageIDs, ecosystems, packageNames, trustedPackageIDs, trustedEcosystems, trustedPackageNames := readinessTargetOwnerArguments(target)
	rows, err := database.QueryContext(
		ctx,
		readinessTargetKeyOwnersQuery,
		array.Of(packageIDs),
		array.Of(ecosystems),
		array.Of(packageNames),
		array.Of(trustedPackageIDs),
		array.Of(trustedEcosystems),
		array.Of(trustedPackageNames),
	)
	if err != nil {
		return fmt.Errorf("check package manifest consumption key ownership: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return rows.Err()
	}
	var valid bool
	if err := rows.Scan(&valid); err != nil {
		return fmt.Errorf("scan package manifest consumption key ownership: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("check package manifest consumption key ownership: %w", err)
	}
	if !valid {
		return fmt.Errorf("package manifest consumption key ownership is ambiguous or incomplete")
	}
	return nil
}

const readinessPackageRegistryTargetQuery = `
SELECT DISTINCT
    COALESCE(fact.payload->>'package_id', ''),
    COALESCE(fact.payload->>'ecosystem', fact.payload->>'package_manager', ''),
    COALESCE(fact.payload->>'raw_name', ''),
    COALESCE(fact.payload->>'normalized_name', ''),
    COALESCE(fact.payload->>'namespace', '')
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'package_registry.package'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->>'package_id' = ANY($1::text[])
`

func loadReadinessRegistryKeysForPackageIDs(
	ctx context.Context,
	database ReadinessQueryer,
	packageIDs []string,
) (map[string][]packageidentity.ConsumptionKey, error) {
	keys := make(map[string][]packageidentity.ConsumptionKey, len(packageIDs))
	if len(packageIDs) == 0 {
		return keys, nil
	}
	rows, err := database.QueryContext(ctx, readinessPackageRegistryTargetQuery, array.Of(packageIDs))
	if err != nil {
		return nil, fmt.Errorf("read active package registry identities: %w", err)
	}
	for rows.Next() {
		var packageID, ecosystem, rawName, normalizedName, namespace string
		if err := rows.Scan(&packageID, &ecosystem, &rawName, &normalizedName, &namespace); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan active package registry identity: %w", err)
		}
		keys[packageID] = append(keys[packageID], packageidentity.ConsumptionKeysForRegistryIdentity(
			ecosystem, rawName, normalizedName, namespace,
		)...)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("read active package registry identities: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close active package registry identities: %w", err)
	}
	return keys, nil
}

func resolveReadinessPackageID(
	ctx context.Context,
	database ReadinessQueryer,
	packageID string,
) (readinessTargetPackage, error) {
	packageID = strings.TrimSpace(packageID)
	if packageID == "" {
		return readinessTargetPackage{}, fmt.Errorf("target package has neither package_id nor PURL")
	}
	registryKeys, err := loadReadinessRegistryKeysForPackageIDs(ctx, database, []string{packageID})
	if err != nil {
		return readinessTargetPackage{}, err
	}
	return readinessTargetPackageForPackageID(packageID, registryKeys[packageID])
}

func readinessTargetPackageForPackageID(
	packageID string,
	registryKeys []packageidentity.ConsumptionKey,
) (readinessTargetPackage, error) {
	packageID = strings.TrimSpace(packageID)
	if packageID == "" {
		return readinessTargetPackage{}, fmt.Errorf("target package has neither package_id nor PURL")
	}
	parserKeys := packageidentity.ConsumptionKeysFromPackageID(packageID)
	result := readinessTargetPackageFromKeys(packageID, parserKeys, true)
	result = mergeReadinessTargetPackage(result, readinessTargetPackageFromKeys(packageID, registryKeys, false))
	if len(result.keys) == 0 {
		return readinessTargetPackage{}, fmt.Errorf("package %q has no unambiguous manifest-consumption identity", packageID)
	}
	return result, nil
}
