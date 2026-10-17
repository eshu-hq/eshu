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

// The deployable_unit_correlation changed-evidence gate (#8283).
//
// A succeeded deployable_unit_correlation item records the input fingerprint
// its corpus-fenced resolved read computed (deployable_unit_reopen_fingerprints,
// migration 193). The deferred-maintenance reopen recomputes that fingerprint
// over the recorded repository set and re-opens the item only when the row is
// missing, the logic version moved, or the fingerprint differs. Both sides
// render the fingerprint from ResolvedInputFingerprintSQL, so the read and the
// reopen can never compute it two different ways.
//
// The fingerprint is the hex sha256 of the sorted resolved_id set of the active resolved
// rows touching the repository set: the rows the handler's fused read returns
// (same join, same status filter, same source-or-target repository match).
// resolved rows are immutable once written (insertResolvedSQL is ON CONFLICT
// DO NOTHING and nothing UPDATEs them), so the id set identifies their
// content. It changes when a dependency scope activates a new generation,
// when a row is appended under an already-active generation (a re-run
// deployment_mapping), when a generation leaves the active set, and when a
// scope the item never read gains a matching row. A generation-id fingerprint
// would miss the append.
//
// Exposure that remains, by design (#8283 ruling, option (i)): a
// deployment_mapping re-opened in the same transaction appends its rows after
// this gate ran, so an item whose input changes that way is re-opened by the
// next pass, not this one. Today's unconditional reopen has the same window
// whenever the item runs before the deployment_mapping. If no later pass runs,
// the item keeps its old graph until one does.

// resolvedInputRepoIDsPlaceholder stands for the text[] repository-set
// expression in resolvedInputRowsSQL: an ARRAY of bind parameters on the fused
// read, the recorded fingerprint row's repo_ids on the reopen listing. It is a
// SQL template placeholder; the name avoids gosec G101's credential pattern.
const resolvedInputRepoIDsPlaceholder = "{{repo_ids}}"

// resolvedInputRowsSQL is the FROM and WHERE of the resolved rows a
// deployable_unit_correlation run reads for a repository set. It mirrors the
// by-repos half of listResolvedByReposWithCorpusFenceSQL (relationship_schema.go)
// with = ANY over an array in place of the IN list; the fingerprint
// differential test proves both select the same rows.
const resolvedInputRowsSQL = `
    FROM resolved_relationships AS input_row
    JOIN relationship_generations AS input_generation
      ON input_generation.generation_id = input_row.generation_id
    WHERE input_generation.status = 'active'
      AND (
        input_row.source_repo_id = ANY(` + resolvedInputRepoIDsPlaceholder + `)
        OR input_row.target_repo_id = ANY(` + resolvedInputRepoIDsPlaceholder + `)
      )`

// ResolvedInputRowsSQL returns resolvedInputRowsSQL with repoIDs as the
// repository-set expression. It is exported for the fingerprint differential
// test, which selects the rows themselves through it.
func ResolvedInputRowsSQL(repoIDs string) string {
	return strings.ReplaceAll(resolvedInputRowsSQL, resolvedInputRepoIDsPlaceholder, repoIDs)
}

// ResolvedInputDigestSQL returns the aggregate that turns a set of resolved_id
// values (the column expression resolvedIDs, over the rows of the enclosing
// FROM) into the input fingerprint: the hex sha256 of the ids sorted under
// the C collation and joined by newlines, or
// reducercontract.EmptyResolvedReadFingerprint for an empty set, so the value
// is never NULL. It is the one digest expression both sides render: the fused
// read over its own materialized rows, the reopen listing over
// resolvedInputRowsSQL. The separator is a newline because resolved_id is a
// prefix-plus-hex digest that never contains one. The digest is sha256, not
// md5: OpenSSL FIPS rejects md5, and production SQL must not call it (#6753,
// TestProductionSQLHasNoMD5Calls).
func ResolvedInputDigestSQL(resolvedIDs string) string {
	return `COALESCE(
      encode(sha256(convert_to(
        string_agg(` + resolvedIDs + `, E'\n' ORDER BY ` + resolvedIDs + ` COLLATE "C"), 'UTF8')), 'hex'),
      '` + reducercontract.EmptyResolvedReadFingerprint + `'
    )`
}

// ResolvedInputFingerprintSQL returns the scalar subquery that computes the
// input fingerprint of repoIDs, a SQL text[] expression, from
// resolvedInputRowsSQL and ResolvedInputDigestSQL.
func ResolvedInputFingerprintSQL(repoIDs string) string {
	return `(
    SELECT ` + ResolvedInputDigestSQL("input_row.resolved_id") + ResolvedInputRowsSQL(repoIDs) + `
  )`
}

// deployableUnitReopenSelect is the gated listing's select list: every
// candidate's id plus whether its input changed. The OR short-circuits, so the
// fingerprint subquery runs only for an item that has a row under the current
// logic version.
var deployableUnitReopenSelect = `SELECT work.work_item_id,
       (fingerprint.work_item_id IS NULL
        OR fingerprint.logic_version IS DISTINCT FROM ` +
	strconv.Itoa(reducercontract.DeployableUnitCorrelationLogicVersion) + `
        OR fingerprint.input_fingerprint IS DISTINCT FROM ` +
	ResolvedInputFingerprintSQL("fingerprint.repo_ids") + `) AS input_changed
`

// deployableUnitReopenFingerprintJoin attaches each candidate's recorded row.
const deployableUnitReopenFingerprintJoin = `LEFT JOIN deployable_unit_reopen_fingerprints AS fingerprint
  ON fingerprint.work_item_id = work.work_item_id
`

// ListDeployableUnitReopenCandidatesQuery is ListSucceededReducerWorkItemsByDomainQuery
// for deployable_unit_correlation with the changed-evidence verdict: the same
// replay floor, failed-generation exclusion, $1 domain line, stage marker
// (exactly once, for the partition-scoped derivation), and order, plus one
// input_changed column. The caller re-opens the rows whose input_changed is
// true and counts the rest as skipped_unchanged. The gate adds no row lock:
// it reads the fingerprint table, and the reopen UPDATE stays the per-row
// ReopenSucceeded it always was.
var ListDeployableUnitReopenCandidatesQuery = scopeReplayFloorCTE +
	deployableUnitReopenSelect +
	scopeReplayFloorJoins +
	deployableUnitReopenFingerprintJoin +
	scopeReplayFloorStage +
	`  AND work.domain = $1
` +
	scopeReplayFloorBound

// RecordDeployableUnitReopenFingerprintQuery stores the read-time
// fingerprint of a deployable_unit_correlation run that is about to succeed.
// The claimant CTE fences it on the exact claim, the same predicate the
// claim-fenced resolution activation uses (work_item_id, last_attempt_at,
// live claim_until), and locks that one work-item row FOR NO KEY UPDATE so a
// concurrent reclaim waits or, if it already committed, drops the row on the
// recheck. The upsert keeps the newest claim: a row recorded by a later claim
// is never replaced by an earlier one. Zero rows affected means the claim no
// longer holds the item. Lock order is the work-item row, then the
// fingerprint row, the same order a retention cascade takes them.
//
// $1 work_item_id, $2 claimed_at (the claim's last_attempt_at), $3 repo_ids,
// $4 input_fingerprint, $5 logic_version.
const RecordDeployableUnitReopenFingerprintQuery = `
WITH claimant AS MATERIALIZED (
    SELECT work_item_id
    FROM fact_work_items
    WHERE work_item_id = $1
      AND stage = 'reducer'
      AND domain = 'deployable_unit_correlation'
      AND status IN ('claimed', 'running')
      AND last_attempt_at = $2
      AND claim_until > clock_timestamp()
    FOR NO KEY UPDATE
)
INSERT INTO deployable_unit_reopen_fingerprints (
    work_item_id, repo_ids, input_fingerprint, logic_version, claimed_at, recorded_at
)
SELECT claimant.work_item_id, $3::text[], $4, $5, $2, clock_timestamp()
FROM claimant
ON CONFLICT (work_item_id) DO UPDATE
SET repo_ids = EXCLUDED.repo_ids,
    input_fingerprint = EXCLUDED.input_fingerprint,
    logic_version = EXCLUDED.logic_version,
    claimed_at = EXCLUDED.claimed_at,
    recorded_at = EXCLUDED.recorded_at
WHERE deployable_unit_reopen_fingerprints.claimed_at <= EXCLUDED.claimed_at
`

// InvalidateDeployableUnitReopenFingerprintsForRepoQuery deletes the reopen
// fingerprint rows of every deployable_unit_correlation item whose recorded
// repository set contains the removed repository (#8316). A missing row
// always re-opens on the next maintenance pass, so deleting the row is the
// repair signal for a graph-side loss the fingerprint gate cannot see.
const InvalidateDeployableUnitReopenFingerprintsForRepoQuery = `DELETE FROM deployable_unit_reopen_fingerprints WHERE $1 = ANY(repo_ids)`

// InvalidateDeployableUnitReopenFingerprintsForReposQuery deletes the same
// rows for a batch of removed repositories in one statement, matched by
// array overlap. Callers with exactly one repository use the single-repo
// form above so its predicate stays the issue's named shape.
const InvalidateDeployableUnitReopenFingerprintsForReposQuery = `
DELETE FROM deployable_unit_reopen_fingerprints WHERE repo_ids && $1::text[]
`

// ListAllDeployableUnitAdmittedAnchorReposQuery lists the full distinct
// source repository set with admitted deployable_unit_correlation decisions
// (#8316). It carries no LIMIT: bounding the listing to a first-N prefix
// would deterministically starve tail anchors, since every pass re-probes
// the same prefix (#8371 review F1). The gap reconciliation probes the graph
// in fixed-size batches instead, so every round trip stays bounded while
// every pass covers the whole set. Rows whose canonical write never landed
// (Written false) are excluded: no edge was ever expected for them. Rows
// predating the Written key are included: every
// deployable_unit_correlation admission of that era wrote its edge.
const ListAllDeployableUnitAdmittedAnchorReposQuery = `
SELECT DISTINCT anchor_id
FROM admission_decisions
WHERE domain = 'deployable_unit_correlation'
  AND state = 'admitted'
  AND anchor_kind = 'repository'
  AND COALESCE((canonical_write ->> 'Written') IN ('true'), TRUE)
ORDER BY anchor_id
`

// ScanDeployableUnitReopenCandidates reads a ListDeployableUnitReopenCandidatesQuery
// result (or its partition-scoped derivation) into the work items to re-open,
// in listing order, and the count left succeeded because their input did not
// change.
func ScanDeployableUnitReopenCandidates(rows db.Rows) ([]string, int, error) {
	defer func() { _ = rows.Close() }()
	reopen := make([]string, 0)
	skipped := 0
	for rows.Next() {
		var workItemID string
		var inputChanged bool
		if err := rows.Scan(&workItemID, &inputChanged); err != nil {
			return nil, 0, fmt.Errorf("scan deployable_unit_correlation reopen candidate: %w", err)
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
		return nil, 0, fmt.Errorf("list deployable_unit_correlation reopen candidates: %w", err)
	}
	return reopen, skipped, nil
}

// ScanDeployableUnitAdmittedAnchorRepos reads a
// ListAllDeployableUnitAdmittedAnchorReposQuery result into the admitted
// source repository ids in listing order, skipping blank ids.
func ScanDeployableUnitAdmittedAnchorRepos(rows db.Rows) ([]string, error) {
	defer func() { _ = rows.Close() }()
	anchors := make([]string, 0)
	for rows.Next() {
		var anchorID string
		if err := rows.Scan(&anchorID); err != nil {
			return nil, fmt.Errorf("scan deployable_unit_correlation admitted anchor: %w", err)
		}
		if strings.TrimSpace(anchorID) == "" {
			continue
		}
		anchors = append(anchors, anchorID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list deployable_unit_correlation admitted anchors: %w", err)
	}
	return anchors, nil
}
