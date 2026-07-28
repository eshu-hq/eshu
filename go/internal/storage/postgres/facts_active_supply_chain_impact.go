// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/environment"
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
      OR (
          cardinality($4::text[]) > 0
          AND (
              fact.payload->>'advisory_id' = ANY($4::text[])
              OR fact.payload->'scope'->>'advisory_id' = ANY($4::text[])
          )
      )
      OR fact.payload->>'subject_digest' = ANY($5::text[])
      OR fact.payload->'scope'->>'subject_digest' = ANY($5::text[])
      OR fact.payload->>'digest' = ANY($5::text[])
      OR fact.payload->>'artifact_digest' = ANY($5::text[])
      OR fact.payload->>'referrer_digest' = ANY($5::text[])
      OR fact.payload->>'resolved_digest' = ANY($5::text[])
      OR fact.payload->>'cpe' = ANY($6::text[])
      OR fact.payload->>'criteria' = ANY($6::text[])
      OR fact.payload->>'document_id' = ANY($7::text[])
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
              fact.payload->>'repository_id' = ANY($8::text[])
              OR fact.payload->>'repo_id' = ANY($8::text[])
              OR fact.payload->'scope'->>'repository_id' = ANY($8::text[])
              OR fact.scope_id = ANY($8::text[])
              OR fact.payload->>'scope_id' = ANY($8::text[])
              OR scope.source_key = ANY($8::text[])
              OR scope.payload->>'repo_id' = ANY($8::text[])
              OR scope.payload->>'id' = ANY($8::text[])
          )
      )
      OR (
          fact.fact_kind = 'file'
          AND (
              fact.payload->>'repository_id' = ANY($10::text[])
              OR fact.payload->>'repo_id' = ANY($10::text[])
              OR fact.payload->'scope'->>'repository_id' = ANY($10::text[])
              OR fact.scope_id = ANY($10::text[])
              OR fact.payload->>'scope_id' = ANY($10::text[])
              OR scope.source_key = ANY($10::text[])
              OR scope.payload->>'repo_id' = ANY($10::text[])
              OR scope.payload->>'id' = ANY($10::text[])
          )
          AND LOWER(COALESCE(
              fact.payload->'parsed_file_data'->>'language',
              fact.payload->>'language',
              ''
          )) IN ('javascript', 'jsx', 'typescript', 'tsx')
      )
      OR fact.payload->>'image_ref' = ANY($9::text[])
      OR (
          fact.fact_kind = 'vulnerability.suppression'
          AND (
              -- lower(trim(...)) matches the decode/matcher contract:
              -- decodeVulnerabilitySuppressionScope only TrimSpaces
              -- workload_id/service_id, and suppressionScopeMatchesFinding
              -- compares them with strings.EqualFold (case-insensitive).
              -- Exact-match SQL against a raw payload value would silently
              -- drop a suppression whose payload casing/whitespace differs
              -- from the filter's already-normalized anchors (#5466 P1-1).
              lower(trim(fact.payload->'scope'->>'workload_id')) = ANY($13::text[])
              OR lower(trim(fact.payload->'scope'->>'service_id')) = ANY($14::text[])
              -- lower(trim(...)) plus alias expansion of $15 (see
              -- expandEnvironmentAliasFilterValues) matches
              -- environment.Canonical's contract exactly:
              -- environment.Canonical is TrimSpace + ToLower + alias, so a
              -- payload authored as " production " (leading/trailing
              -- whitespace, alias spelling) must still be selected by a
              -- canonical "prod" filter, not just an exact-canonical,
              -- untrimmed payload (#5466 P1-1, tightened by round-2 review
              -- F-1: the two sibling lines above already needed trim() for
              -- the same reason).
              OR lower(trim(fact.payload->'scope'->>'environment')) = ANY($15::text[])
          )
      )
  )
  AND ($11 = '' OR fact.fact_id > $11)
ORDER BY fact.fact_id ASC
LIMIT $12
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
	filter.AdvisoryIDs = cleanStringFilterValues(filter.AdvisoryIDs)
	filter.SubjectDigests = cleanStringFilterValues(filter.SubjectDigests)
	filter.DocumentIDs = cleanStringFilterValues(filter.DocumentIDs)
	filter.ProductCriteria = cleanStringFilterValues(filter.ProductCriteria)
	filter.RepositoryIDs = cleanStringFilterValues(filter.RepositoryIDs)
	filter.FileRepositoryIDs = cleanStringFilterValues(filter.FileRepositoryIDs)
	filter.ImageRefs = cleanStringFilterValues(filter.ImageRefs)
	filter.WorkloadIDs = cleanStringFilterValues(filter.WorkloadIDs)
	filter.ServiceIDs = cleanStringFilterValues(filter.ServiceIDs)
	filter.Environments = cleanStringFilterValues(filter.Environments)
	if len(filter.PackageIDs) == 0 && len(filter.PURLs) == 0 &&
		len(filter.CVEIDs) == 0 && len(filter.AdvisoryIDs) == 0 && len(filter.SubjectDigests) == 0 &&
		len(filter.DocumentIDs) == 0 && len(filter.ProductCriteria) == 0 &&
		len(filter.RepositoryIDs) == 0 && len(filter.FileRepositoryIDs) == 0 &&
		len(filter.ImageRefs) == 0 && len(filter.WorkloadIDs) == 0 &&
		len(filter.ServiceIDs) == 0 && len(filter.Environments) == 0 {
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
		filter.AdvisoryIDs,
		filter.SubjectDigests,
		filter.ProductCriteria,
		filter.DocumentIDs,
		filter.RepositoryIDs,
		filter.ImageRefs,
		filter.FileRepositoryIDs,
		cursorFactID,
		listFactsByKindPageSize,
		// $12/$13 are compared against lower(trim(payload)) in SQL, so the
		// bind values must be lowercased/trimmed the same way -- otherwise a
		// filter anchor with different case than the payload (both valid
		// under the matcher's EqualFold contract) would never match.
		lowerCleanedStringFilterValues(filter.WorkloadIDs),
		lowerCleanedStringFilterValues(filter.ServiceIDs),
		// $14 is compared against lower(payload) plus every known alias of
		// each canonical filter environment (see
		// expandEnvironmentAliasFilterValues), so a suppression payload
		// authored with an alias form ("production") still matches a
		// canonical "prod" filter.
		expandEnvironmentAliasFilterValues(filter.Environments),
	)
	if err != nil {
		return nil, fmt.Errorf("list active supply chain impact facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0,
		len(filter.PackageIDs)+len(filter.PURLs)+len(filter.CVEIDs)+len(filter.AdvisoryIDs)+len(filter.SubjectDigests)+
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

// lowerCleanedStringFilterValues lowercases every value and re-runs
// cleanStringFilterValues (trim, drop-empty, dedupe, sort) so the result is a
// stable bind-ready set. Used for the workload_id/service_id suppression
// scope anchors ($12/$13), which the SQL predicate compares against
// lower(trim(payload)) to match decodeVulnerabilitySuppressionScope's
// TrimSpace-only decode and the matcher's case-insensitive
// (strings.EqualFold) comparison (#5466 P1-1).
func lowerCleanedStringFilterValues(values []string) []string {
	lowered := make([]string, 0, len(values))
	for _, value := range values {
		lowered = append(lowered, strings.ToLower(value))
	}
	return cleanStringFilterValues(lowered)
}

// expandEnvironmentAliasFilterValues canonicalizes each environment value in
// values through environment.Canonical, then expands it into every alias
// spelling from the shared environment.Aliases() table (which always
// includes the canonical spelling itself -- see the table in
// environment.Aliases()). The suppression-scope environment predicate
// ($14) compares against lower(trim(payload)), so a suppression payload
// authored with an alias form ("production") or different case/whitespace
// ("Prod") still matches a filter built from the canonical form ("prod")
// -- otherwise the filter and the payload could each independently be
// "correct" under environment.Canonical and still never match in SQL
// (#5466 P1-1). Canonicalizing here rather than trusting the caller already
// did so keeps this function correct regardless of caller input, matching
// lowerCleanedStringFilterValues's defensive normalization of its own
// input. A canonical value with no known alias entry (environment.Canonical
// never rejects unknown tokens) passes through as its canonicalized form.
func expandEnvironmentAliasFilterValues(values []string) []string {
	if len(values) == 0 {
		return values
	}
	aliasesByCanonical := make(map[string][]string, len(environment.Aliases()))
	for _, entry := range environment.Aliases() {
		aliasesByCanonical[entry.Canonical] = entry.Aliases
	}
	expanded := make([]string, 0, len(values))
	for _, value := range values {
		canonical := environment.Canonical(value)
		if aliases, ok := aliasesByCanonical[canonical]; ok {
			expanded = append(expanded, aliases...)
			continue
		}
		expanded = append(expanded, canonical)
	}
	return cleanStringFilterValues(expanded)
}
