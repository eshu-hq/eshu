// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Writer SQL of the staged winners audit sweep (#11131): the persisted cursor
// and the per-range verify. One range commits in one writer transaction: the
// pending filter, the verify and its repairs, and the compare-and-set of the
// cursor. A lost compare-and-set rolls the repairs back.

// winnersAuditRangePendingTable holds the candidates already queued, found
// before the lock is taken. The CREATE spells the name out for the DDL parity
// guard; TestWinnersTempTableDDLNamesMatchTheTableConstants pins both.
const winnersAuditRangePendingTable = "winners_audit_range_pending"

// winnersAuditRestrictedPendingSelect returns the identifiers of table (five
// columns) that are already waiting to be consumed: a dirty-log or staging
// fact row with the same identifier, or a fact of the identifier in a
// (scope, generation) a queued scope row names. It is the set
// createWinnersAuditPendingSQL builds, restricted to table's identifiers, so
// a range verify never expands the whole queue: the fact arm probes the
// dirty log's unique fact-key index, the scope arm probes the keyed winners
// index once per identifier, and staging (which has no identifier index) is
// read by one hash join against the small identifier set.
func winnersAuditRestrictedPendingSelect(table string) string {
	ident := `(i.package_id, i.repository_id, i.subject_digest, i.c0, i.advisory_id)`
	return `SELECT i.package_id, i.repository_id, i.subject_digest, i.c0, i.advisory_id
FROM ` + table + ` AS i
JOIN supply_chain_impact_winners_dirty_log AS d
  ON d.kind = 'fact'
 AND (d.package_id, d.repository_id, d.subject_digest, d.c0, d.advisory_id) = ` + ident + `
UNION
SELECT i.package_id, i.repository_id, i.subject_digest, i.c0, i.advisory_id
FROM ` + table + ` AS i
JOIN ` + winnersStagingTable + ` AS s
  ON s.kind = 'fact'
 AND (s.package_id, s.repository_id, s.subject_digest, s.c0, s.advisory_id) = ` + ident + `
UNION
SELECT i.package_id, i.repository_id, i.subject_digest, i.c0, i.advisory_id
FROM ` + table + ` AS i
CROSS JOIN LATERAL (
    SELECT f.scope_id, f.generation_id
    FROM fact_records AS f
    WHERE (f.payload->>'package_id') = i.package_id
      AND (f.payload->>'repository_id') = i.repository_id
      AND (f.payload->>'subject_digest') = i.subject_digest
      AND COALESCE(NULLIF(f.payload->>'cve_id', ''), NULLIF(f.payload->>'advisory_id', ''), '') = i.c0
      AND COALESCE(f.payload->>'advisory_id', '') = i.advisory_id
      AND f.fact_kind = 'reducer_supply_chain_impact_finding'
      AND f.is_tombstone = FALSE
    OFFSET 0
) AS f
JOIN (
    SELECT scope_id, generation_id FROM supply_chain_impact_winners_dirty_log WHERE kind = 'scope'
    UNION
    SELECT scope_id, generation_id FROM ` + winnersStagingTable + ` WHERE kind = 'scope'
) AS p
  ON p.scope_id = f.scope_id
 AND p.generation_id = f.generation_id`
}

// createWinnersAuditRangePendingSQL finds, before any lock, the range
// candidates a keyed tick will recompute anyway.
var createWinnersAuditRangePendingSQL = `
CREATE TEMP TABLE winners_audit_range_pending ON COMMIT DROP AS
` + winnersAuditRestrictedPendingSelect(winnersKeyedIdentsTable)

// createWinnersAuditRangeStalePendingSQL rechecks, under the lock, which of
// the verified stale identifiers were queued since the pre-lock filter. The
// table carries the name queueWinnersAuditRepairsSQL reads.
var createWinnersAuditRangeStalePendingSQL = `
CREATE TEMP TABLE winners_audit_pending ON COMMIT DROP AS
` + winnersAuditRestrictedPendingSelect(winnersAuditStaleTable)
