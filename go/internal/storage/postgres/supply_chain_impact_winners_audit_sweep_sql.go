// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"time"
)

// Writer SQL of the staged winners audit sweep (#11131): the persisted cursor
// and the per-range verify. One range commits in one writer transaction: the
// pending filter, the verify and its repairs, and the compare-and-set of the
// cursor. A lost compare-and-set rolls the repairs back.

// WinnersAuditRangeLockTimeout bounds the waiting resweep-lock claim a range
// verify falls back to after two try-lock misses on the same range. It is
// lock_timeout, not a statement timeout: a keyed tick that holds the lock
// longer makes the claim fail with SQLSTATE 55P03, recorded as lock_busy,
// and the range retries. The value is provisional until the measured keyed
// tick maximum (theory measurement M8 of the #11131 ruling) replaces it.
const WinnersAuditRangeLockTimeout = 20 * time.Second

// winnersAuditSweepTable is the singleton cursor row (migration 331).
const winnersAuditSweepTable = "supply_chain_impact_winners_audit_sweep"

// winnersAuditRangePendingTable holds the candidates already queued, found
// before the lock is taken. The CREATE spells the name out for the DDL parity
// guard; TestWinnersTempTableDDLNamesMatchTheTableConstants pins both.
const winnersAuditRangePendingTable = "winners_audit_range_pending"

// selectWinnersAuditSweepSQL reads the cursor row, creating it on first use.
// The CTE's insert is invisible to the outer read, so the UNION returns one
// row whether the insert ran or the row already existed.
const selectWinnersAuditSweepSQL = `
WITH created AS (
    INSERT INTO ` + winnersAuditSweepTable + ` (singleton) VALUES (1)
    ON CONFLICT (singleton) DO NOTHING
    RETURNING sweep_id, cursor_package_id, cursor_repository_id, cursor_subject_digest, cursor_c0, cursor_advisory_id,
        ranges_done, sweep_started_at, last_sweep_completed_at, last_sweep_seconds, last_sweep_ranges,
        last_sweep_facts, last_sweep_candidates, last_sweep_repairs, last_sweep_discards
)
SELECT * FROM created
UNION ALL
SELECT sweep_id, cursor_package_id, cursor_repository_id, cursor_subject_digest, cursor_c0, cursor_advisory_id,
    ranges_done, sweep_started_at, last_sweep_completed_at, last_sweep_seconds, last_sweep_ranges,
    last_sweep_facts, last_sweep_candidates, last_sweep_repairs, last_sweep_discards
FROM ` + winnersAuditSweepTable + `
WHERE singleton = 1`

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

// dropWinnersAuditRangePendingIdentsSQL removes those candidates from the
// identifier table the verify probes, so the lock is taken only when a
// candidate that nothing has queued remains.
const dropWinnersAuditRangePendingIdentsSQL = `
DELETE FROM ` + winnersKeyedIdentsTable + ` AS i
USING ` + winnersAuditRangePendingTable + ` AS p
WHERE (i.package_id, i.repository_id, i.subject_digest, i.c0, i.advisory_id)
    = (p.package_id, p.repository_id, p.subject_digest, p.c0, p.advisory_id)`

// countWinnersAuditRangeRemainingSQL counts the candidates left to verify.
const countWinnersAuditRangeRemainingSQL = `SELECT count(*) FROM ` + winnersKeyedIdentsTable

// createWinnersAuditRangeStalePendingSQL rechecks, under the lock, which of
// the verified stale identifiers were queued since the pre-lock filter. The
// table carries the name queueWinnersAuditRepairsSQL reads.
var createWinnersAuditRangeStalePendingSQL = `
CREATE TEMP TABLE winners_audit_pending ON COMMIT DROP AS
` + winnersAuditRestrictedPendingSelect(winnersAuditStaleTable)

// setWinnersAuditRangeLockTimeoutSQL scopes the waiting claim's bound to the
// range transaction.
var setWinnersAuditRangeLockTimeoutSQL = fmt.Sprintf("SET LOCAL lock_timeout = '%dms'", WinnersAuditRangeLockTimeout.Milliseconds())

// waitClaimSupplyChainImpactWinnersResweepSQL is the waiting form of the
// resweep lock claim, on the same key as tryClaimSupplyChainImpactWinnersResweepSQL.
// PostgreSQL grants a released lock to a queued waiter before a later
// try-lock can take it, so a verify queued here wins the next release.
const waitClaimSupplyChainImpactWinnersResweepSQL = `
SELECT pg_advisory_xact_lock(hashtext('supply_chain_impact_winners'), hashtext('resweep'))
`

// advanceWinnersAuditSweepSQL is the compare-and-set of the cursor. It moves
// the cursor from $2..$6 (NULL at sweep start) to $7..$11 (NULL when the
// range was the last) only while sweep_id is still $1 and the stored cursor
// still equals $2..$6. Zero rows means another instance advanced this range
// first: the caller rolls its repairs back. $12 ends the sweep (the last
// range); $13 also records it as completed (the end NULL probe read clean).
// $14..$17 are the range's facts, candidates, repairs and discards.
const advanceWinnersAuditSweepSQL = `
UPDATE ` + winnersAuditSweepTable + ` SET
    sweep_id = CASE WHEN $12::boolean THEN sweep_id + 1 ELSE sweep_id END,
    cursor_package_id = $7::text,
    cursor_repository_id = $8::text,
    cursor_subject_digest = $9::text,
    cursor_c0 = $10::text,
    cursor_advisory_id = $11::text,
    sweep_started_at = CASE WHEN $12::boolean THEN NULL ELSE COALESCE(sweep_started_at, clock_timestamp()) END,
    ranges_done = CASE WHEN $12::boolean THEN 0 ELSE ranges_done + 1 END,
    sweep_facts = CASE WHEN $12::boolean THEN 0 ELSE sweep_facts + $14::bigint END,
    sweep_candidates = CASE WHEN $12::boolean THEN 0 ELSE sweep_candidates + $15::bigint END,
    sweep_repairs = CASE WHEN $12::boolean THEN 0 ELSE sweep_repairs + $16::bigint END,
    sweep_discards = CASE WHEN $12::boolean THEN 0 ELSE sweep_discards + $17::bigint END,
    last_sweep_completed_at = CASE WHEN $13::boolean THEN clock_timestamp() ELSE last_sweep_completed_at END,
    last_sweep_seconds = CASE WHEN $13::boolean
        THEN EXTRACT(EPOCH FROM clock_timestamp() - COALESCE(sweep_started_at, clock_timestamp()))::double precision
        ELSE last_sweep_seconds END,
    last_sweep_ranges = CASE WHEN $13::boolean THEN ranges_done + 1 ELSE last_sweep_ranges END,
    last_sweep_facts = CASE WHEN $13::boolean THEN sweep_facts + $14::bigint ELSE last_sweep_facts END,
    last_sweep_candidates = CASE WHEN $13::boolean THEN sweep_candidates + $15::bigint ELSE last_sweep_candidates END,
    last_sweep_repairs = CASE WHEN $13::boolean THEN sweep_repairs + $16::bigint ELSE last_sweep_repairs END,
    last_sweep_discards = CASE WHEN $13::boolean THEN sweep_discards + $17::bigint ELSE last_sweep_discards END,
    updated_at = clock_timestamp()
WHERE singleton = 1
  AND sweep_id = $1::bigint
  AND ROW(cursor_package_id, cursor_repository_id, cursor_subject_digest, cursor_c0, cursor_advisory_id)
      IS NOT DISTINCT FROM ROW($2::text, $3::text, $4::text, $5::text, $6::text)
RETURNING sweep_id, ranges_done, last_sweep_completed_at, last_sweep_seconds`
