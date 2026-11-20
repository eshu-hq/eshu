// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"fmt"
	"strconv"
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The aws_cloud_runtime_drift changed-evidence gate (#10878, first domain).
//
// A succeeded aws_cloud_runtime_drift item records the ARN snapshot its
// evidence load read (aws_drift_reopen_fingerprints, migration 317): the
// scope and generation, the sorted ARN set, the per-ARN payload identity,
// and the input fingerprint over both. The deferred-maintenance reopen
// recomputes that fingerprint over the scope's CURRENT active generation
// with the same SQL fragment and re-opens the item only when the row is
// missing, the logic version moved, or the fingerprint differs. Both sides
// render the fingerprint from AWSDriftARNFingerprintSQL, so the read and
// the reopen can never compute it two different ways. The Go twin of the
// digest is contract.ComputeARNInputFingerprint: the record path loads the
// per-ARN hashes with AWSDriftSnapshotRowsSQL (the same expression) and
// fingerprints them in Go, so the stored value matches what the listing
// recomputes in SQL for identical input.
//
// The per-ARN payload identity is the hex sha256 of the fact payload text.
// fact payloads are jsonb, whose text output is deterministic for equal
// values (keys normalized), so equal facts hash equal and any content
// change rehashes. The digest is sha256, not md5: OpenSSL FIPS rejects md5
// in production SQL (#6753, TestProductionSQLHasNoMD5Calls).
//
// A generation-id fingerprint would miss an append under an already-active
// generation (the #8283 option (i) lesson); the ARN+payload fingerprint
// catches appends, removals, and in-place content changes alike.

// awsDriftPayloadHashSQL renders the per-ARN payload identity over the
// payload column expression payloadExpr.
func awsDriftPayloadHashSQL(payloadExpr string) string {
	return `encode(sha256(convert_to(` + payloadExpr + `::text, 'UTF8')), 'hex')`
}

// AWSDriftARNFingerprintSQL returns the scalar subquery that computes the
// input fingerprint of one AWS scope generation: the hex sha256 of the
// sorted "arn newline payload-hash" pairs joined by newlines, or
// reducercontract.EmptyARNInputFingerprint for an empty set, so the value
// is never NULL. scopeIDExpr and generationIDExpr are SQL text expressions
// (bind parameters or column references). The separator is a newline
// because neither an ARN nor a hex digest contains one.
func AWSDriftARNFingerprintSQL(scopeIDExpr, generationIDExpr string) string {
	return `COALESCE((
    SELECT encode(sha256(convert_to(
      string_agg(pair.arn || E'\n' || pair.payload_hash, E'\n' ORDER BY pair.arn COLLATE "C"), 'UTF8')), 'hex')
    FROM (
      SELECT fact.payload->>'arn' AS arn,
             ` + awsDriftPayloadHashSQL("fact.payload") + ` AS payload_hash
      FROM fact_records AS fact
      WHERE fact.scope_id = ` + scopeIDExpr + `
        AND fact.generation_id = ` + generationIDExpr + `
        AND fact.fact_kind = 'aws_resource'
        AND btrim(COALESCE(fact.payload->>'arn', '')) <> ''
      GROUP BY fact.payload->>'arn', ` + awsDriftPayloadHashSQL("fact.payload") + `
    ) AS pair
  ), '` + reducercontract.EmptyARNInputFingerprint + `')`
}

// AWSDriftSnapshotRowsSQL lists one AWS scope generation's ARN snapshot:
// one row per ARN with its payload identity, in ARN order. The record path
// stores these rows; the next run diffs them against its fresh load to
// narrow to changed ARNs. $1 scope_id, $2 generation_id.
const AWSDriftSnapshotRowsSQL = `
SELECT fact.payload->>'arn' AS arn,
       encode(sha256(convert_to(fact.payload::text, 'UTF8')), 'hex') AS payload_hash
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind = 'aws_resource'
  AND btrim(COALESCE(fact.payload->>'arn', '')) <> ''
GROUP BY fact.payload->>'arn', encode(sha256(convert_to(fact.payload::text, 'UTF8')), 'hex')
ORDER BY fact.payload->>'arn' ASC
`

// awsDriftReopenSelect is the gated listing's select list: every
// candidate's id plus whether its input changed. The OR short-circuits, so
// the fingerprint subquery runs only for an item that has a row under the
// current logic version. The current fingerprint renders over the scope's
// active generation: a supersession carrying different ARNs (or different
// payloads) mismatches the recorded value and re-opens the item.
var awsDriftReopenSelect = `SELECT work.work_item_id,
       (fingerprint.work_item_id IS NULL
        OR fingerprint.logic_version IS DISTINCT FROM ` +
	strconv.Itoa(reducercontract.AWSRuntimeDriftLogicVersion) + `
        OR fingerprint.arn_fingerprint IS DISTINCT FROM ` +
	AWSDriftARNFingerprintSQL("work.scope_id", "scope.active_generation_id") + `) AS input_changed
`

// awsDriftReopenFingerprintJoin attaches each candidate's recorded row plus
// the scope row carrying the current active generation.
const awsDriftReopenFingerprintJoin = `JOIN ingestion_scopes AS scope
  ON scope.scope_id = work.scope_id
LEFT JOIN aws_drift_reopen_fingerprints AS fingerprint
  ON fingerprint.work_item_id = work.work_item_id
`

// ListAWSDriftReopenCandidatesQuery is ListSucceededReducerWorkItemsByDomainQuery
// for aws_cloud_runtime_drift with the changed-evidence verdict: the same
// replay floor, failed-generation exclusion, $1 domain line, stage marker,
// and order, plus one input_changed column. The caller re-opens the rows
// whose input_changed is true and counts the rest as skipped_unchanged.
var ListAWSDriftReopenCandidatesQuery = scopeReplayFloorCTE +
	awsDriftReopenSelect +
	scopeReplayFloorJoins +
	awsDriftReopenFingerprintJoin +
	scopeReplayFloorStage +
	`  AND work.domain = $1
` +
	scopeReplayFloorBound

// RecordAWSDriftReopenFingerprintQuery stores the ARN snapshot of an
// aws_cloud_runtime_drift run that is about to succeed. The claimant CTE
// fences it on the exact claim, the same predicate the claim-fenced
// resolution activation uses (work_item_id, last_attempt_at, live
// claim_until), and locks that one work-item row FOR NO KEY UPDATE so a
// concurrent reclaim waits or, if it already committed, drops the row on
// the recheck. The upsert keeps the newest claim: a row recorded by a
// later claim is never replaced by an earlier one. Zero rows affected
// means the claim no longer holds the item.
//
// $1 work_item_id, $2 claimed_at (the claim's last_attempt_at), $3
// scope_id, $4 generation_id, $5 arn_ids, $6 arn_payload_hashes, $7
// arn_fingerprint, $8 logic_version.
const RecordAWSDriftReopenFingerprintQuery = `
WITH claimant AS MATERIALIZED (
    SELECT work_item_id
    FROM fact_work_items
    WHERE work_item_id = $1
      AND stage = 'reducer'
      AND domain = 'aws_cloud_runtime_drift'
      AND status IN ('claimed', 'running')
      AND last_attempt_at = $2
      AND claim_until > clock_timestamp()
    FOR NO KEY UPDATE
)
INSERT INTO aws_drift_reopen_fingerprints (
    work_item_id, scope_id, generation_id, arn_ids, arn_payload_hashes,
    arn_fingerprint, logic_version, claimed_at, recorded_at
)
SELECT claimant.work_item_id, $3, $4, $5::text[], $6::text[], $7, $8, $2, clock_timestamp()
FROM claimant
ON CONFLICT (work_item_id) DO UPDATE
SET scope_id = EXCLUDED.scope_id,
    generation_id = EXCLUDED.generation_id,
    arn_ids = EXCLUDED.arn_ids,
    arn_payload_hashes = EXCLUDED.arn_payload_hashes,
    arn_fingerprint = EXCLUDED.arn_fingerprint,
    logic_version = EXCLUDED.logic_version,
    claimed_at = EXCLUDED.claimed_at,
    recorded_at = EXCLUDED.recorded_at
WHERE aws_drift_reopen_fingerprints.claimed_at <= EXCLUDED.claimed_at
`

// ScanAWSDriftReopenCandidates reads a ListAWSDriftReopenCandidatesQuery
// result into the work items to re-open, in listing order, and the count
// left succeeded because their input did not change.
func ScanAWSDriftReopenCandidates(rows db.Rows) ([]string, int, error) {
	defer func() { _ = rows.Close() }()
	reopen := make([]string, 0)
	skipped := 0
	for rows.Next() {
		var workItemID string
		var inputChanged bool
		if err := rows.Scan(&workItemID, &inputChanged); err != nil {
			return nil, 0, fmt.Errorf("scan aws_cloud_runtime_drift reopen candidate: %w", err)
		}
		if strings.TrimSpace(workItemID) == "" {
			continue
		}
		if !inputChanged {
			skipped++
			continue
		}
		reopen = append(reopen, workItemID)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list aws_cloud_runtime_drift reopen candidates: %w", err)
	}
	return reopen, skipped, nil
}
