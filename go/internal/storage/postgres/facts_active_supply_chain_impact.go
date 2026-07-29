// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// suppressionScopeTrimCharactersSQL is PostgreSQL's explicit spelling of the
// Unicode White_Space set used by strings.TrimSpace. Keeping the sets equal
// prevents the SQL prefilter from making a suppression inert before the Go
// decoder can apply its matching contract.
const suppressionScopeTrimCharactersSQL = `U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'`

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
      -- $13-$17 (below and at repository_id) are lower(btrim(...))
      -- normalized REPLACEMENTS for what used to be exact-match
      -- ->'scope'->>'X' = ANY($N) predicates here (there is no exact-match
      -- fallback left for package_id/purl/cve_id/subject_digest/
      -- repository_id under "scope" -- $13-$17 fully supersede them, added
      -- for #5466 round-3 review F-6: scopeAnchorMatches
      -- (go/internal/reducer/supply_chain_suppression_scope_match.go)
      -- compares every vulnerability.suppression scope anchor with
      -- strings.TrimSpace + strings.EqualFold, so a payload of
      -- {"cve_id":"cve-2026-1234"} (lowercase) or a whitespace-padded purl
      -- decodes and matches in Go but was never SELECTED by the prior
      -- exact-match SQL -- the same defect class P1-1/F-4 fixed for
      -- environment/workload_id/service_id, now closed here too. These use
      -- NEW placeholders rather than reusing $1-$3/$5/$8 because those
      -- placeholders are ALSO bound to the top-level (non-"scope") sibling
      -- predicates immediately below, which serve OTHER fact kinds
      -- (vulnerability.affected_package, sbom.component, ...) whose
      -- existing exact-match behavior must not change -- only the
      -- "scope"-nested comparisons were replaced.
      OR (
          fact.fact_kind = 'vulnerability.suppression'
          AND cardinality($13::text[]) > 0
          AND lower(btrim(fact.payload->'scope'->>'package_id', ` + suppressionScopeTrimCharactersSQL + `)) = ANY($13::text[])
      )
      OR fact.payload->>'purl' = ANY($2::text[])
      OR (
          fact.fact_kind = 'vulnerability.suppression'
          AND cardinality($14::text[]) > 0
          AND lower(btrim(fact.payload->'scope'->>'purl', ` + suppressionScopeTrimCharactersSQL + `)) = ANY($14::text[])
      )
      OR fact.payload->>'cve_id' = ANY($3::text[])
      OR (
          fact.fact_kind = 'vulnerability.suppression'
          AND cardinality($15::text[]) > 0
          AND lower(btrim(fact.payload->'scope'->>'cve_id', ` + suppressionScopeTrimCharactersSQL + `)) = ANY($15::text[])
      )
      OR (
          cardinality($4::text[]) > 0
          AND (
              fact.payload->>'advisory_id' = ANY($4::text[])
          )
      )
      OR fact.payload->>'subject_digest' = ANY($5::text[])
      OR (
          fact.fact_kind = 'vulnerability.suppression'
          AND cardinality($16::text[]) > 0
          AND lower(btrim(fact.payload->'scope'->>'subject_digest', ` + suppressionScopeTrimCharactersSQL + `)) = ANY($16::text[])
      )
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
              -- $17 is the lower(btrim(...)) normalized replacement for
              -- what was an exact-match ->'scope'->>'repository_id' =
              -- ANY($8) predicate here. It binds the same repository filter
              -- values after normalization, so it strictly supersedes the
              -- old exact-match comparison (#5466 round-3 review F-6).
              OR (
                  fact.fact_kind = 'vulnerability.suppression'
                  AND cardinality($17::text[]) > 0
                  AND lower(btrim(fact.payload->'scope'->>'repository_id', ` + suppressionScopeTrimCharactersSQL + `)) = ANY($17::text[])
              )
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
          -- $18 replaces the exact nested advisory_id comparison with the
          -- same case-folding and Unicode TrimSpace semantics the reducer
          -- applies. Deployment context is deliberately absent here:
          -- environment, workload_id, and service_id only narrow
          -- suppressions discovered through an identity anchor.
          AND cardinality($18::text[]) > 0
          AND lower(btrim(fact.payload->'scope'->>'advisory_id', ` + suppressionScopeTrimCharactersSQL + `)) = ANY($18::text[])
      )
  )
  AND (
      $11 = ''
      OR (fact.fact_kind = 'vulnerability.suppression', fact.fact_id) > ($19::boolean, $11)
  )
-- #5466 round-8 review F-3: every non-suppression row sorts before every
-- vulnerability.suppression row (the boolean ASC term), so the row cap
-- below (ListActiveSupplyChainImpactFacts) can bound ONLY the suppression
-- tail without ever truncating vulnerability.cve/affected_package/sbom.
-- component/... evidence -- see that function's doc for the full
-- reasoning. The compound keyset cursor ($19 + $11, a Postgres row-value
-- comparison) is required to paginate correctly against this two-part
-- ordering; a plain "fact.fact_id > $11" cursor would skip or repeat rows
-- once pagination crosses from the non-suppression group into the
-- suppression group.
ORDER BY (fact.fact_kind = 'vulnerability.suppression') ASC, fact.fact_id ASC
LIMIT $12
`

// maxSupplyChainImpactActiveEvidenceRowsPerCall bounds the vulnerability.
// suppression rows one ListActiveSupplyChainImpactFacts call may return
// before it stops paginating and reports truncation (#5466 round-7 review
// P1-B, round-8 review F-3). Without this cap, a suppression matching a
// broad identity filter with no other constraint could paginate through a
// large active set.
//
// This counts ONLY vulnerability.suppression rows, never core evidence
// (vulnerability.cve/affected_package/sbom.component/...): the query orders
// every non-suppression row before every suppression row (ORDER BY
// (fact_kind = 'vulnerability.suppression') ASC, fact_id ASC), so by the
// time this cap can possibly fire, every matching non-suppression row for
// the call's filter has already been loaded in full -- core evidence for a
// finding can never be crowded out of the result by suppression noise
// sharing the same filter values, regardless of how many suppression rows
// exist. The 2,000-row ceiling is four existing 500-row pages of
// suppression evidence, generous headroom for the operator-authored
// suppression counts this query realistically serves (round-3/round-6
// EXPLAIN evidence, gotchas-and-invariants.md). The loader admits exactly
// this many suppression rows, then reads one sentinel before reporting
// truncation; an exact-cap result with no sentinel is complete.
//
// A truncated load fails OPEN for the entire suppression candidate set: the
// returned bool reaches SupplyChainImpactHandler, which discards the retained
// prefix before evaluation because it cannot prove that prefix contains the
// globally preferred AuthoredAt/ID winner. Core evidence remains usable and
// the same bool surfaces through the active-evidence truncation signal. A
// `var`, not a `const`, so hermetic tests can lower it without seeding
// thousands of rows.
var maxSupplyChainImpactActiveEvidenceRowsPerCall = 2000

// ListActiveSupplyChainImpactFacts loads active package, SBOM, image, and
// risk evidence for one bounded supply-chain impact reducer intent. The
// bool return reports whether maxSupplyChainImpactActiveEvidenceRowsPerCall
// truncated the vulnerability.suppression tail before every matching
// suppression row was loaded -- core (non-suppression) evidence is never
// truncated by this cap; see that var's doc for why.
func (s FactStore) ListActiveSupplyChainImpactFacts(
	ctx context.Context,
	filter reducer.SupplyChainImpactFactFilter,
) ([]facts.Envelope, bool, error) {
	if s.db == nil {
		return nil, false, fmt.Errorf("fact store database is required")
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
	if len(filter.PackageIDs) == 0 && len(filter.PURLs) == 0 &&
		len(filter.CVEIDs) == 0 && len(filter.AdvisoryIDs) == 0 && len(filter.SubjectDigests) == 0 &&
		len(filter.DocumentIDs) == 0 && len(filter.ProductCriteria) == 0 &&
		len(filter.RepositoryIDs) == 0 && len(filter.FileRepositoryIDs) == 0 &&
		len(filter.ImageRefs) == 0 {
		return nil, false, nil
	}

	var loaded []facts.Envelope
	var suppressionLoaded int
	var cursorFactID string
	var cursorIsSuppression bool
	for {
		page, err := s.listActiveSupplyChainImpactFactsPage(ctx, filter, cursorFactID, cursorIsSuppression)
		if err != nil {
			return nil, false, err
		}
		for _, envelope := range page {
			if envelope.FactKind == facts.VulnerabilitySuppressionFactKind {
				if suppressionLoaded == maxSupplyChainImpactActiveEvidenceRowsPerCall {
					return loaded, true, nil
				}
				suppressionLoaded++
			}
			loaded = append(loaded, envelope)
		}
		if len(page) < listFactsByKindPageSize {
			return loaded, false, nil
		}
		last := page[len(page)-1]
		cursorFactID = last.FactID
		cursorIsSuppression = last.FactKind == facts.VulnerabilitySuppressionFactKind
	}
}

func (s FactStore) listActiveSupplyChainImpactFactsPage(
	ctx context.Context,
	filter reducer.SupplyChainImpactFactFilter,
	cursorFactID string,
	cursorIsSuppression bool,
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
		// $13-$17 are lower(btrim(...)) normalized siblings of the
		// exact-match ->'scope'->>'package_id'/'purl'/'cve_id'/
		// 'subject_digest'/'repository_id' predicates bound to $1-$3/$5/$8
		// (#5466 round-3 review F-6; no exact-match fallback remains for
		// these "scope"-nested comparisons). They reuse the SAME filter
		// values as $1/$2/$3/$5/$8 -- normalized here, not by mutating
		// filter.* -- because $1-$3/$5/$8 are ALSO bound to the top-level
		// (non-"scope") sibling predicates serving other fact kinds, whose exact-match
		// behavior must not change.
		lowerCleanedStringFilterValues(filter.PackageIDs),
		lowerCleanedStringFilterValues(filter.PURLs),
		lowerCleanedStringFilterValues(filter.CVEIDs),
		lowerCleanedStringFilterValues(filter.SubjectDigests),
		lowerCleanedStringFilterValues(filter.RepositoryIDs),
		// $18 is the lower(btrim(...)) normalized replacement for #5465's
		// exact nested advisory_id comparison. The same AdvisoryIDs values
		// still bind the top-level exact-match predicate at $4.
		lowerCleanedStringFilterValues(filter.AdvisoryIDs),
		// $19 is the second half of the compound keyset cursor (#5466
		// round-8 review F-3): paired with $11 (cursorFactID), it resumes
		// pagination correctly against the query's two-part ORDER BY
		// (non-suppression rows before suppression rows, each fact_id-
		// ordered). Ignored by the query when $11 = '' (first page).
		cursorIsSuppression,
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
// stable bind-ready set for normalized suppression identity predicates.
func lowerCleanedStringFilterValues(values []string) []string {
	lowered := make([]string, 0, len(values))
	for _, value := range values {
		lowered = append(lowered, strings.ToLower(value))
	}
	return cleanStringFilterValues(lowered)
}
