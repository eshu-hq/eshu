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
      -- $16-$20 (below and at L~repository_id) are lower(btrim(...))
      -- normalized REPLACEMENTS for what used to be exact-match
      -- ->'scope'->>'X' = ANY($N) predicates here (there is no exact-match
      -- fallback left for package_id/purl/cve_id/subject_digest/
      -- repository_id under "scope" -- $16-$20 fully supersede them, added
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
      OR lower(btrim(fact.payload->'scope'->>'package_id', E' \t\n\v\f\r')) = ANY($16::text[])
      OR fact.payload->>'purl' = ANY($2::text[])
      OR lower(btrim(fact.payload->'scope'->>'purl', E' \t\n\v\f\r')) = ANY($17::text[])
      OR fact.payload->>'cve_id' = ANY($3::text[])
      OR lower(btrim(fact.payload->'scope'->>'cve_id', E' \t\n\v\f\r')) = ANY($18::text[])
      OR (
          cardinality($4::text[]) > 0
          AND (
              fact.payload->>'advisory_id' = ANY($4::text[])
          )
      )
      OR fact.payload->>'subject_digest' = ANY($5::text[])
      OR lower(btrim(fact.payload->'scope'->>'subject_digest', E' \t\n\v\f\r')) = ANY($19::text[])
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
              -- $20 is the lower(btrim(...)) normalized replacement for
              -- what was an exact-match ->'scope'->>'repository_id' =
              -- ANY($8) predicate here. It binds the same repository filter
              -- values after normalization, so it strictly supersedes the
              -- old exact-match comparison (#5466 round-3 review F-6).
              OR lower(btrim(fact.payload->'scope'->>'repository_id', E' \t\n\v\f\r')) = ANY($20::text[])
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
              -- lower(btrim(x, E' \t\n\v\f\r')) approximates -- it does NOT
              -- exactly match -- the decode/matcher contract:
              -- decodeVulnerabilitySuppressionScope reads workload_id/
              -- service_id through payloadStr, which TrimSpaces, and
              -- environment through environment.Canonical, which
              -- TrimSpace+ToLower+alias-resolves (payloadStr's TrimSpace
              -- runs first, so environment is TrimSpace'd twice); the
              -- matcher then compares all three with strings.EqualFold
              -- (case-insensitive). Go's strings.TrimSpace strips the full
              -- Unicode White_Space property (tab, newline, NBSP U+00A0,
              -- the U+2000-200A run, U+2028/2029, U+202F, U+205F, U+3000,
              -- ...); Postgres's trim()/btrim() only strips a literal
              -- character set, with no built-in "trim Unicode whitespace"
              -- primitive, so the explicit E' \t\n\v\f\r' class below only
              -- covers the ASCII control-whitespace set (space, tab,
              -- newline, vertical tab, form feed, carriage return) --
              -- covers realistic operator-authored payloads (including
              -- tab/newline padding, #5466 round-2 review F-4) but a
              -- payload padded with an exotic non-ASCII Unicode space
              -- character would still decode/match in Go and not be
              -- selected here. This residual gap is accepted, not silently
              -- assumed away.
              lower(btrim(fact.payload->'scope'->>'workload_id', E' \t\n\v\f\r')) = ANY($13::text[])
              OR lower(btrim(fact.payload->'scope'->>'service_id', E' \t\n\v\f\r')) = ANY($14::text[])
              -- Same lower(btrim(...)) treatment plus alias expansion of
              -- $15 (see expandEnvironmentAliasFilterValues): a payload
              -- authored as " production " (ASCII whitespace padding,
              -- alias spelling) must still be selected by a canonical
              -- "prod" filter (#5466 P1-1, tightened by round-2 review
              -- F-1/F-4 -- see the whitespace-class caveat above, which
              -- applies here identically).
              OR lower(btrim(fact.payload->'scope'->>'environment', E' \t\n\v\f\r')) = ANY($15::text[])
              -- $21 is the lower(btrim(...)) normalized REPLACEMENT for
              -- what was a dead scope key: advisory_id had NO load-path
              -- predicate at all before #5466 round-4 review F-10.
              -- vulnerability.cve/affected_package carry a raw top-level
              -- advisory_id (indexed by
              -- fact_records_vulnerability_active_advisory_lookup_v2_idx --
              -- vulnerability.affected_product shares that index's
              -- fact_kind list but does NOT carry the field itself, #5466
              -- round-5 review F-12), but supplyChainCVEID prefers cve_id
              -- over advisory_id (firstNonBlank), so a suppression scoped
              -- ONLY by advisory_id (e.g. a GHSA ID distinct from any
              -- cve_id) was unreachable by this query even though
              -- scopeAnchorMatches accepts it and
              -- suppressionScopeIsEmpty/the reasons string both advertise
              -- advisory_id as a sufficient sole anchor.
              OR lower(btrim(fact.payload->'scope'->>'advisory_id', E' \t\n\v\f\r')) = ANY($21::text[])
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
		// $13/$14 are compared against lower(btrim(payload, ASCII
		// whitespace)) in SQL (see the query text above for the exact
		// character class and its residual-gap caveat), so the bind values
		// -- already Unicode-TrimSpace'd on the Go side by
		// lowerCleanedStringFilterValues -- must be lowercased the same way
		// -- otherwise a filter anchor with different case than the payload
		// (both valid under the matcher's EqualFold contract) would never
		// match.
		lowerCleanedStringFilterValues(filter.WorkloadIDs),
		lowerCleanedStringFilterValues(filter.ServiceIDs),
		// $15 is compared against lower(btrim(payload, ASCII whitespace))
		// plus every known alias of each canonical filter environment (see
		// expandEnvironmentAliasFilterValues), so a suppression payload
		// authored with an alias form ("production") still matches a
		// canonical "prod" filter.
		expandEnvironmentAliasFilterValues(filter.Environments),
		// $16-$20 are lower(btrim(...)) normalized siblings of the
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
		// $21 is the lower(btrim(...)) normalized replacement for what was
		// a dead scope key -- advisory_id had no load-path predicate at all
		// before #5466 round-4 review F-10 (see the query text above).
		lowerCleanedStringFilterValues(filter.AdvisoryIDs),
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
// scope anchors ($13/$14), which the SQL predicate compares against
// lower(btrim(payload, ASCII whitespace)) to approximate
// decodeVulnerabilitySuppressionScope's TrimSpace-only decode and the
// matcher's case-insensitive (strings.EqualFold) comparison (#5466 P1-1) --
// see the query text's whitespace-class comment for the residual
// non-ASCII-whitespace gap this does not close.
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
// ($15) compares against lower(btrim(payload, ASCII whitespace)), so a
// suppression payload authored with an alias form ("production") or
// different case/ASCII-whitespace ("Prod", " production ") still matches a
// filter built from the canonical form ("prod") -- otherwise the filter and
// the payload could each independently be "correct" under
// environment.Canonical and still never match in SQL (#5466 P1-1;
// non-ASCII Unicode whitespace padding is a documented residual gap, see
// the query text's whitespace-class comment). Canonicalizing here rather
// than trusting the caller already did so keeps this function correct
// regardless of caller input, matching lowerCleanedStringFilterValues's
// defensive normalization of its own input. A canonical value with no known
// alias entry (environment.Canonical never rejects unknown tokens) passes
// through as its canonicalized form.
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
