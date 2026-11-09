// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package advisory

// This file holds the SQL half of the newest-copy rule for GHSA rows
// (eshu-hq/eshu#10464, design 10037 section 4): for one payload source and
// advisory id, the copy with the greatest modified_at is the record, and an
// unparseable or empty timestamp ranks oldest. A recency tie breaks
// live-beats-retracted through the closed retraction classifier, and only a
// same-lifecycle tie breaks on the greater fact id — the reducer's
// newestCopyAfter in SQL. Older copies leave the group whole: their
// vulnerability.cve and vulnerability.affected_package facts both go, so a
// package the source removed from an advisory stops matching.
//
// Both call sites alias the filtered row as fact and read fact.payload,
// fact.fact_kind, fact.scope_id, and fact.generation_id: the catalog legs in
// catalog_sql.go (applied at scan time so the single GROUP BY pass keeps its
// bounded shape) and the evidence seed and matched selects in
// evidence_sql.go. The winners CTE is spliced into each WITH chain ahead of
// its first use.

// AdvisoryGHSARFC3339Pattern is a family-local copy of the impact reader's
// readinessRFC3339Pattern (readiness_postgres_query_targets.go): the strict
// RFC 3339 shape the collector's time.Parse(time.RFC3339) accepts. It must
// stay behavior-identical to its root source; a value without a zone would
// resolve in the session TimeZone, and hour 24 or second 60 parse in
// Postgres but not in Go, so each is malformed here. Calendar validity is
// left to pg_input_is_valid. Exported only for the parity guard in the
// impact package's rfc3339_parity_test.go: do not import it elsewhere.
const AdvisoryGHSARFC3339Pattern = `^[0-9]{4}-[0-9]{2}-[0-9]{2}T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]+)?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`

// advisoryGHSACopyNormSQL names one advisory record's copy stream the way
// the reducer keys it: the payload source is matched separately, and the id
// falls back from advisory_id through ghsa_id to cve_id so a copy that
// never stated its recency still groups with its record. A copy with no id
// at all reads NULL and is kept fail-open by the keep predicate: the read
// never hides evidence it cannot attribute.
const advisoryGHSACopyNormSQL = `UPPER(TRIM(COALESCE(NULLIF(TRIM(fact.payload->>'advisory_id'), ''), NULLIF(TRIM(fact.payload->>'ghsa_id'), ''), NULLIF(TRIM(fact.payload->>'cve_id'), ''))))`

// advisoryGHSACopyModifiedSQL ranks one copy's recency: the greatest
// parseable modified_at wins, and an empty or unparseable timestamp ranks
// oldest (NULLS LAST at election), so a copy that never stated its recency
// never beats one that did.
const advisoryGHSACopyModifiedSQL = `CASE WHEN NULLIF(TRIM(fact.payload->>'modified_at'), '') ~ '` + AdvisoryGHSARFC3339Pattern + `' AND pg_input_is_valid(TRIM(fact.payload->>'modified_at'), 'timestamp with time zone') THEN (TRIM(fact.payload->>'modified_at'))::timestamptz ELSE NULL END`

// advisoryGHSACopyRetractedSQL is the closed retraction classifier the
// reducer's advisoryRetractionCode applies: a rejected or withdrawn status
// (case-insensitive, trimmed), or any non-blank withdrawn_at, retracts, and
// any other status retracts nothing. The COALESCE keeps a copy that states
// neither field on the live side of the tiebreak instead of sorting NULL
// first.
const advisoryGHSACopyRetractedSQL = `COALESCE(LOWER(TRIM(fact.payload->>'status')) IN ('rejected', 'withdrawn') OR NULLIF(TRIM(fact.payload->>'withdrawn_at'), '') IS NOT NULL, FALSE)`

// advisoryGHSANewestCopyCTE elects the winning copy per advisory record for
// GHSA-sourced vulnerability.cve facts: DISTINCT ON the advisory id, newest
// modified first with unparseable oldest, live before retracted, greater
// fact id last. winner_has_package records whether the winning scope holds
// an affected_package fact of its own, so package evidence that lives only
// in a superseded copy's scope still reads when the winner names none.
const advisoryGHSANewestCopyCTE = `ghsa_newest_copy AS (
    SELECT DISTINCT ON (ranked.advisory_norm) ranked.advisory_norm, ranked.scope_id, ranked.generation_id,
        EXISTS (
            SELECT 1 FROM fact_records AS pkg
            JOIN ingestion_scopes AS pscope
              ON pkg.scope_id = pscope.scope_id
             AND pscope.active_generation_id = pkg.generation_id
            JOIN scope_generations AS pgeneration
              ON pgeneration.scope_id = pkg.scope_id
             AND pgeneration.generation_id = pkg.generation_id
            WHERE pkg.fact_kind = 'vulnerability.affected_package'
              AND pkg.is_tombstone = FALSE
              AND pgeneration.status = 'active'
              AND LOWER(TRIM(pkg.payload->>'source')) = 'ghsa'
              AND UPPER(TRIM(COALESCE(NULLIF(TRIM(pkg.payload->>'advisory_id'), ''), NULLIF(TRIM(pkg.payload->>'ghsa_id'), ''), NULLIF(TRIM(pkg.payload->>'cve_id'), '')))) = ranked.advisory_norm
              AND pkg.scope_id = ranked.scope_id
              AND pkg.generation_id = ranked.generation_id
        ) AS winner_has_package
    FROM (
        SELECT ` + advisoryGHSACopyNormSQL + ` AS advisory_norm,
            fact.scope_id, fact.generation_id, fact.fact_id AS fact_id,
            ` + advisoryGHSACopyModifiedSQL + ` AS modified_ts,
            ` + advisoryGHSACopyRetractedSQL + ` AS retracted
        FROM fact_records AS fact
        JOIN ingestion_scopes AS scope
          ON fact.scope_id = scope.scope_id
         AND scope.active_generation_id = fact.generation_id
        JOIN scope_generations AS generation
          ON generation.scope_id = fact.scope_id
         AND generation.generation_id = fact.generation_id
        WHERE fact.fact_kind = 'vulnerability.cve'
          AND fact.is_tombstone = FALSE
          AND generation.status = 'active'
          AND LOWER(TRIM(fact.payload->>'source')) = 'ghsa'
    ) AS ranked
    WHERE ranked.advisory_norm IS NOT NULL
    ORDER BY ranked.advisory_norm, ranked.modified_ts DESC NULLS LAST, ranked.retracted ASC, ranked.fact_id DESC
),
`

// advisoryGHSACopyKeepPredicate keeps every row except a superseded GHSA
// copy: a GHSA vulnerability.cve or vulnerability.affected_package fact goes
// unless its scope and generation won the election for its advisory. A
// package fact stays when its advisory names no CVE record (an unmatched
// live observation the reduction must not hide), when its own scope and
// generation hold no same-key live CVE fact (the reducer's !present[key][own]
// keep in newestCopyAdvisoryInputs, keyed by exact payload source and
// advisory id, e.g. a scope whose CVE copy was tombstoned), and when the
// winning copy names no package of its own (the record's package evidence
// lives only in the superseded copy's scope).
const advisoryGHSACopyKeepPredicate = `(
        fact.fact_kind NOT IN ('vulnerability.cve', 'vulnerability.affected_package')
        OR LOWER(TRIM(fact.payload->>'source')) IS DISTINCT FROM 'ghsa'
        OR ` + advisoryGHSACopyNormSQL + ` IS NULL
        OR (fact.scope_id, fact.generation_id) IN (
            SELECT w.scope_id, w.generation_id FROM ghsa_newest_copy AS w
            WHERE w.advisory_norm = ` + advisoryGHSACopyNormSQL + `
        )
        OR (fact.fact_kind = 'vulnerability.affected_package'
            AND NOT EXISTS (
                SELECT 1 FROM ghsa_newest_copy AS w
                WHERE w.advisory_norm = ` + advisoryGHSACopyNormSQL + `
            ))
        OR (fact.fact_kind = 'vulnerability.affected_package'
            AND EXISTS (
                SELECT 1 FROM ghsa_newest_copy AS w
                WHERE w.advisory_norm = ` + advisoryGHSACopyNormSQL + `
                  AND NOT w.winner_has_package
            ))
        OR (fact.fact_kind = 'vulnerability.affected_package'
            AND NOT EXISTS (
                SELECT 1 FROM fact_records AS own_cve
                WHERE own_cve.fact_kind = 'vulnerability.cve'
                  AND own_cve.is_tombstone = FALSE
                  AND own_cve.scope_id = fact.scope_id
                  AND own_cve.generation_id = fact.generation_id
                  AND (own_cve.payload->>'source') = (fact.payload->>'source')
                  AND (own_cve.payload->>'advisory_id') = (fact.payload->>'advisory_id')
            ))
    )`
