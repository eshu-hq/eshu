// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"fmt"
	"strings"
)

// The input fingerprint (#8283) names what a deployable_unit_correlation item
// read when it succeeded, so the deferred-maintenance reopen can skip an item
// whose inputs have not changed. It is a generation fingerprint, not a content
// hash: it covers the relationship generations the item's reads depend on and
// is computed by SQL on both sides.
//
// There is exactly ONE definition of the value, InputFingerprintExprSQL. The
// fused fenced read in the parent package evaluates it in the same statement
// snapshot as the corpus fence and the resolved rows, and
// ListSucceededDeployableUnitWorkItemsQuery evaluates it again against the
// stored repo ids. Go never hashes anything and never compares a Go-computed
// value with a SQL-computed one: the recorded value is the opaque string the
// fused read returned.
//
// The value is the hex SHA-256 of two parts. It is SHA-256 and not md5 because the
// production database runs with FIPS-mode OpenSSL, where md5() fails with
// "could not compute MD5 hash: unsupported" (checked on the production reader,
// where sha256() works):
//
//   - the item's OWN relationship generation: id, status, activated_at (UTC,
//     microseconds, so a session TimeZone cannot change the text) and the count
//     of its resolved rows. A missing generation row reads '-'.
//   - every ACTIVE relationship generation, in any scope, that holds a resolved
//     row touching one of the stored repository ids: id, activated_at, and the
//     count of those touching rows, ordered by id.
//
// Any activation (including the ON CONFLICT re-stamp), supersession without a
// replacement, a new generation touching the repositories, or rows added to a
// generation changes the value. Each moves a generation id, an activated_at, a
// status, or a row count; resolved rows are inserted ON CONFLICT DO NOTHING, so
// counts only grow within a generation and reset deletes a whole generation.
const inputFingerprintTimestampSQL = `COALESCE(to_char(%s AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US'), '')`

// InputFingerprintExprSQL returns the scalar SQL expression of the input
// fingerprint. ownGenerationExpr is a text-valued expression naming the item's
// own relationship generation; repoIDsExpr is a text[]-valued expression of the
// candidate repository ids. Both are spliced in textually, so the caller owns
// their syntax (a bind placeholder or a column reference).
func InputFingerprintExprSQL(ownGenerationExpr, repoIDsExpr string) string {
	activatedOwn := fmt.Sprintf(inputFingerprintTimestampSQL, "own.activated_at")
	activatedTouching := fmt.Sprintf(inputFingerprintTimestampSQL, "g.activated_at")
	return `encode(sha256(convert_to(concat_ws('|',
  COALESCE((
    SELECT own.generation_id || '/' || own.status || '/' || ` + activatedOwn + ` || '/' ||
           (SELECT count(*) FROM resolved_relationships AS own_rows
            WHERE own_rows.generation_id = own.generation_id)::text
    FROM relationship_generations AS own
    WHERE own.generation_id = ` + ownGenerationExpr + `
  ), '-'),
  COALESCE((
    SELECT string_agg(t.generation_id || '/' || t.activated_text || '/' || t.row_count::text,
                      ',' ORDER BY t.generation_id)
    FROM (
      SELECT g.generation_id,
             ` + activatedTouching + ` AS activated_text,
             count(*) AS row_count
      FROM resolved_relationships AS r
      JOIN relationship_generations AS g
        ON g.generation_id = r.generation_id
      WHERE g.status = 'active'
        AND (
          r.source_repo_id = ANY(` + repoIDsExpr + `)
          OR r.target_repo_id = ANY(` + repoIDsExpr + `)
        )
      GROUP BY g.generation_id, g.activated_at
    ) AS t
  ), '-')
), 'UTF8')), 'hex')`
}

// ListSucceededDeployableUnitWorkItemsQuery is the correlation reopen listing
// for deployable_unit_correlation (#8283). It is
// ListSucceededReducerWorkItemsByDomainQuery with the same replay floor,
// failed-generation exclusion, and order, plus a second column: unchanged is
// true only when the item has a recorded fingerprint row AND the fingerprint
// recomputed now, from the item's stored repo_ids and its own generation, still
// equals the recorded one. A missing row is false, so the item reopens (fail
// open toward accuracy). The CASE keeps the correlated fingerprint expression
// off the rows that have no recorded row.
//
// The column is a flag, not a filter: every succeeded item above the floor is
// still returned, so the caller can count what it skipped. $1 is the domain.
//
// The expression runs inside the pass's reopen transaction, one READ COMMITTED
// statement snapshot. An activation that commits after that snapshot is seen by
// the next pass, exactly like the blanket reopen it replaces.
var ListSucceededDeployableUnitWorkItemsQuery = scopeReplayFloorCTE +
	`SELECT work.work_item_id,
       CASE
         WHEN fingerprint.work_item_id IS NULL THEN false
         ELSE fingerprint.fingerprint = ` +
	InputFingerprintExprSQL("work.generation_id", "fingerprint.repo_ids") + `
       END AS unchanged
` +
	scopeReplayFloorJoins +
	`LEFT JOIN reducer_work_input_fingerprints AS fingerprint
  ON fingerprint.work_item_id = work.work_item_id
` +
	scopeReplayFloorWhereStage +
	`  AND work.domain = $1
` +
	scopeReplayFloorBound

// ListResolvedByReposWithCorpusFenceAndInputFingerprintSQL returns the fused
// fenced by-repos read of listResolvedByReposWithCorpusFenceSQL with the input
// fingerprint added in the SAME statement. The fence verdict, the resolved
// rows, and the fingerprint therefore share one READ COMMITTED statement
// snapshot: a fingerprint taken in a second statement could pair a later
// generation state with earlier rows, and the item would then be skipped
// although its rows were read before the change.
//
// The fingerprint CTE is MATERIALIZED and guarded by the fence verdict, so it
// is evaluated once and not at all when the corpus is incomplete (the caller
// defers and records nothing). corpusFencePredicate is the parent package's
// incompleteScopeRelationshipGenerationsPredicate, passed in because this
// package never imports its parent. The inner read is checked against the plain
// statement by TestFingerprintedFusedReadKeepsThePlainReadBody.
//
// Bind $1..$n are the candidate repository ids; $n+1 is the own relationship
// generation id.
func ListResolvedByReposWithCorpusFenceAndInputFingerprintSQL(corpusFencePredicate string, repoCount int) string {
	repoPlaceholders := make([]string, repoCount)
	repoArrayElems := make([]string, repoCount)
	for i := range repoPlaceholders {
		repoPlaceholders[i] = fmt.Sprintf("$%d", i+1)
		repoArrayElems[i] = fmt.Sprintf("$%d::text", i+1)
	}
	placeholderList := strings.Join(repoPlaceholders, ", ")
	repoArray := "ARRAY[" + strings.Join(repoArrayElems, ", ") + "]"
	ownGeneration := fmt.Sprintf("$%d::text", repoCount+1)
	return fmt.Sprintf(fusedFingerprintReadTemplate,
		corpusFencePredicate,
		InputFingerprintExprSQL(ownGeneration, repoArray),
		placeholderList, placeholderList)
}

// fusedFingerprintReadTemplate is the fused statement with %s holes for the
// fence predicate, the fingerprint expression, and the two repo id lists.
const fusedFingerprintReadTemplate = `
WITH fence AS MATERIALIZED (
  SELECT NOT EXISTS (
    SELECT 1
    FROM ingestion_scopes AS s
    WHERE%s
  ) AS complete
),
fingerprint AS MATERIALIZED (
  SELECT %s AS value
  WHERE (SELECT complete FROM fence)
)
SELECT fence.complete,
       COALESCE(fingerprint.value, ''),
       r.source_repo_id, r.target_repo_id,
       r.source_key, r.target_key,
       r.relationship_type,
       COALESCE(r.confidence, 0), COALESCE(r.evidence_count, 0),
       COALESCE(r.rationale, ''), COALESCE(r.resolution_source, ''), r.details
FROM fence
LEFT JOIN fingerprint ON true
LEFT JOIN (
  SELECT r.source_repo_id, r.target_repo_id,
         COALESCE(r.source_entity_id, r.source_repo_id) AS source_key,
         COALESCE(r.target_entity_id, r.target_repo_id) AS target_key,
         r.relationship_type, r.confidence, r.evidence_count,
         r.rationale, r.resolution_source, r.details
  FROM resolved_relationships AS r
  JOIN relationship_generations AS g
    ON g.generation_id = r.generation_id
  WHERE g.status = 'active'
    AND (
      r.source_repo_id IN (%s)
      OR r.target_repo_id IN (%s)
    )
) AS r ON fence.complete
ORDER BY r.source_key, r.target_key, r.relationship_type
`
