// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const listActiveSupplyChainImpactFactsQuery = `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind IN (
    'vulnerability.cve',
    'vulnerability.affected_package',
    'vulnerability.affected_product',
    'vulnerability.suppression',
    'security_alert.repository_alert',
    'package_registry.package_version',
    'package_registry.vulnerability_hint',
    'reducer_package_consumption_correlation',
    'sbom.component',
    'reducer_sbom_attestation_attachment',
    'reducer_container_image_identity',
    'reducer_ci_cd_run_correlation',
    'reducer_platform_materialization',
    'reducer_service_catalog_correlation',
    'reducer_workload_identity',
    'oci_registry.image_manifest',
    'oci_registry.image_index',
    'oci_registry.image_tag_observation',
    'oci_registry.image_referrer',
    'file',
    'vulnerability.epss_score',
    'vulnerability.known_exploited'
)
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
      fact.payload->>'package_id' = ANY($1::text[])
      OR fact.payload->'scope'->>'package_id' = ANY($1::text[])
      OR fact.payload->>'purl' = ANY($2::text[])
      OR fact.payload->'scope'->>'purl' = ANY($2::text[])
      OR fact.payload->>'cve_id' = ANY($3::text[])
      OR fact.payload->'scope'->>'cve_id' = ANY($3::text[])
      OR fact.payload->>'subject_digest' = ANY($4::text[])
      OR fact.payload->'scope'->>'subject_digest' = ANY($4::text[])
      OR fact.payload->>'digest' = ANY($4::text[])
      OR fact.payload->>'artifact_digest' = ANY($4::text[])
      OR fact.payload->>'referrer_digest' = ANY($4::text[])
      OR fact.payload->>'resolved_digest' = ANY($4::text[])
      OR fact.payload->>'cpe' = ANY($5::text[])
      OR fact.payload->>'criteria' = ANY($5::text[])
      OR fact.payload->>'document_id' = ANY($6::text[])
      OR (
          fact.fact_kind IN (
              'vulnerability.suppression',
              'reducer_package_consumption_correlation',
              'reducer_container_image_identity',
              'reducer_ci_cd_run_correlation',
              'reducer_platform_materialization',
              'reducer_service_catalog_correlation',
              'reducer_workload_identity'
          )
          AND (
              fact.payload->>'repository_id' = ANY($7::text[])
              OR fact.payload->>'repo_id' = ANY($7::text[])
              OR fact.payload->'scope'->>'repository_id' = ANY($7::text[])
              OR fact.scope_id = ANY($7::text[])
              OR fact.payload->>'scope_id' = ANY($7::text[])
              OR scope.source_key = ANY($7::text[])
              OR scope.payload->>'repo_id' = ANY($7::text[])
              OR scope.payload->>'id' = ANY($7::text[])
          )
      )
      OR (
          fact.fact_kind = 'file'
          AND (
              fact.payload->>'repository_id' = ANY($9::text[])
              OR fact.payload->>'repo_id' = ANY($9::text[])
              OR fact.payload->'scope'->>'repository_id' = ANY($9::text[])
              OR fact.scope_id = ANY($9::text[])
              OR fact.payload->>'scope_id' = ANY($9::text[])
              OR scope.source_key = ANY($9::text[])
              OR scope.payload->>'repo_id' = ANY($9::text[])
              OR scope.payload->>'id' = ANY($9::text[])
          )
          AND LOWER(COALESCE(
              fact.payload->'parsed_file_data'->>'language',
              fact.payload->>'language',
              ''
          )) IN ('javascript', 'jsx', 'typescript', 'tsx')
      )
      OR fact.payload->>'image_ref' = ANY($8::text[])
  )
  AND ($10 = '' OR fact.fact_id > $10)
ORDER BY fact.fact_id ASC
LIMIT $11
`

// ListActiveSupplyChainImpactFacts loads active package, SBOM, image, and risk
// evidence for one bounded supply-chain impact reducer intent.
func (s FactStore) ListActiveSupplyChainImpactFacts(
	ctx context.Context,
	filter reducer.SupplyChainImpactFactFilter,
) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	filter.PackageIDs = cleanStringFilterValues(filter.PackageIDs)
	filter.PURLs = cleanStringFilterValues(filter.PURLs)
	filter.CVEIDs = cleanStringFilterValues(filter.CVEIDs)
	filter.SubjectDigests = cleanStringFilterValues(filter.SubjectDigests)
	filter.DocumentIDs = cleanStringFilterValues(filter.DocumentIDs)
	filter.ProductCriteria = cleanStringFilterValues(filter.ProductCriteria)
	filter.RepositoryIDs = cleanStringFilterValues(filter.RepositoryIDs)
	filter.FileRepositoryIDs = cleanStringFilterValues(filter.FileRepositoryIDs)
	filter.ImageRefs = cleanStringFilterValues(filter.ImageRefs)
	if len(filter.PackageIDs) == 0 && len(filter.PURLs) == 0 &&
		len(filter.CVEIDs) == 0 && len(filter.SubjectDigests) == 0 &&
		len(filter.DocumentIDs) == 0 && len(filter.ProductCriteria) == 0 &&
		len(filter.RepositoryIDs) == 0 && len(filter.FileRepositoryIDs) == 0 &&
		len(filter.ImageRefs) == 0 {
		return nil, nil
	}

	var loaded []facts.Envelope
	var cursorFactID string
	for {
		page, err := s.listActiveSupplyChainImpactFactsPage(ctx, filter, cursorFactID)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, page...)
		if len(page) < listFactsByKindPageSize {
			return loaded, nil
		}
		cursorFactID = page[len(page)-1].FactID
	}
}

func (s FactStore) listActiveSupplyChainImpactFactsPage(
	ctx context.Context,
	filter reducer.SupplyChainImpactFactFilter,
	cursorFactID string,
) ([]facts.Envelope, error) {
	rows, err := s.db.QueryContext(
		ctx,
		listActiveSupplyChainImpactFactsQuery,
		filter.PackageIDs,
		filter.PURLs,
		filter.CVEIDs,
		filter.SubjectDigests,
		filter.ProductCriteria,
		filter.DocumentIDs,
		filter.RepositoryIDs,
		filter.ImageRefs,
		filter.FileRepositoryIDs,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list active supply chain impact facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0,
		len(filter.PackageIDs)+len(filter.PURLs)+len(filter.CVEIDs)+len(filter.SubjectDigests)+
			len(filter.DocumentIDs)+len(filter.ProductCriteria)+len(filter.FileRepositoryIDs))
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active supply chain impact facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active supply chain impact facts: %w", err)
	}
	return loaded, nil
}
