// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Shared recovery replay templates: the operator and recovery-plane replay
// of terminal (dead_letter, failed) rows to pending (#11182, #11615). Every
// replay variant shares one UPDATE body so the skipped-row exclusion can
// never be dropped by a missing hand-written variant. Reducer rows take the
// closed-vocabulary requeue reason through the stage gate; projector rows
// keep NULL by retaining their prior value (#11615 decision 3).

// replaySkippedProjectorCTE is the body of the skipped CTE for a projector
// replay: it counts the terminal rows matching the filter that the replay fence
// leaves in place. %s is the unfenced predicate, with the same placeholders the
// replay uses, so the count adds no argument. The fenced rows are never touched
// by the replay UPDATE, so the count is exact in the statement's snapshot.
const replaySkippedProjectorCTE = `SELECT COUNT(*) AS n FROM fact_work_items
    WHERE status IN ('dead_letter', 'failed')
      %s
      AND ` + supersededProjectorGenerationFence

// replaySkippedNoneCTE is the skipped CTE body for a non-projector replay: the
// fence is projector-only, so the count is a constant and costs no scan.
const replaySkippedNoneCTE = `SELECT 0::bigint AS n`

// replayFailedWorkItemsTemplate resets matching terminal rows to pending. The
// first %s is replaced by the dynamic predicate built from the replay filter
// (scope, failure class, the manual-review exclusion, and the superseded-
// generation fence), so every replay variant shares one UPDATE body and the
// exclusion can never be dropped by a missing hand-written variant. The second
// %s is the skipped CTE body (replaySkippedProjectorCTE or
// replaySkippedNoneCTE). $1 is the replay timestamp; the predicate
// placeholders start at $2.
//
// The locked CTE takes every row lock first, ordered by work_item_id COLLATE
// "C" like the batch ack and the own-gate wake (#8205, #7597): this replay
// waits on locks, and in index-scan order two overlapping replays took shared
// rows in opposite orders (40P01). FOR NO KEY UPDATE is the UPDATE's own lock.
// Both CTE and UPDATE recheck terminal status, which the EvalPlanQual recheck
// of a waited lock re-reads on the newest row version, so a row another
// transaction made pending or claimed is dropped, not reset twice. The UPDATE
// reads the ids through = ANY(ARRAY(...)) to probe the primary key; IN (SELECT
// ...) planned a scan of every terminal row. AS MATERIALIZED measured no plan
// change. See (#8205).
//
// The replay stamps reopened_at with the replay timestamp (#9320): every row it
// touches was dead_letter or failed, so it becomes outstanding again and its
// backlog age (COALESCE(reopened_at, created_at)) and reducer readiness-grace
// anchor (Intent.CycleStartedAt) start from the replay, the same reset the
// own-gate activation wake (activationWakeSetClause) applies.
//
// The result carries one skip count per row, and a replay that moved nothing
// still returns one row with a NULL work_item_id, so the count commits or
// fails together with the replay (#7130 review).
var replayFailedWorkItemsTemplate = `
WITH skipped AS (
    %[2]s
), locked AS (
    SELECT work_item_id
    FROM fact_work_items
    WHERE status IN ('dead_letter', 'failed')
      %[1]s
    ORDER BY work_item_id COLLATE "C"
    FOR NO KEY UPDATE
), replayed AS (
    UPDATE fact_work_items
    SET status = 'pending',
        attempt_count = GREATEST(attempt_count, 1),
        container_image_identity_v2_authorized_status = CASE
            WHEN container_image_identity_v2_required THEN 'pending'
            ELSE ''
        END,
        container_image_identity_v3_authorized_status = CASE
            WHEN container_image_identity_v3_required THEN 'pending'
            ELSE ''
        END,
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = $1,
        next_attempt_at = NULL,
        failure_class = NULL,
        failure_message = NULL,
        failure_details = NULL,
        updated_at = $1,
        reopened_at = $1,
        last_requeue_reason = ` + queuestore.RequeueReasonStageGatedValue(
	telemetry.RequeueReasonRecoveryRedrive.SQLLiteral(), "") + `,
        ` + queuestore.ReplayRetryAllowanceAssignments + `
    WHERE work_item_id = ANY(ARRAY(SELECT work_item_id FROM locked))
      AND status IN ('dead_letter', 'failed')
    RETURNING work_item_id
)
SELECT replayed.work_item_id, skipped.n
FROM skipped
LEFT JOIN replayed ON true
ORDER BY replayed.work_item_id
`

// replayFailedWorkItemsBoundedTemplate is the limited replay variant for the
// dead-letter backlog drain (#3560, #3652 P3). It mutates at most $2 terminal
// rows by selecting their primary keys in a bounded candidate set first, so a
// Limit=100 drain against thousands of retry_exhausted rows replays exactly 100
// rows instead of resetting every matching row to pending and recreating the
// write surge the drain exists to avoid.
//
// The candidate set is a MATERIALIZED CTE so the bounded select evaluates
// exactly once: inlined into the UPDATE's IN list, the planner may re-execute
// it per outer row, and each execution locks the next still-terminal row,
// replaying past the Limit (measured 2 rows under Limit=1). ORDER BY keeps
// forward progress deterministic; SKIP LOCKED keeps concurrent drains safe.
//
// FOR UPDATE SKIP LOCKED locks only the chosen rows and skips rows another
// concurrent drain already holds, so two drains never fight over the same rows
// and the bound stays a true cap under concurrent execution rather than a
// serialization point.
//
// The first %s is the shared replay predicate and the second is the skipped
// CTE body, exactly as in replayFailedWorkItemsTemplate. $1 is the replay
// timestamp, which also stamps reopened_at as in the unbounded replay; $2 is
// the row limit; the predicate placeholders start at $3.
var replayFailedWorkItemsBoundedTemplate = `
WITH skipped AS (
    %[2]s
), candidates AS MATERIALIZED (
    SELECT work_item_id FROM fact_work_items
    WHERE status IN ('dead_letter', 'failed')
      %[1]s
    ORDER BY work_item_id
    LIMIT $2
    FOR UPDATE SKIP LOCKED
), replayed AS (
    UPDATE fact_work_items
    SET status = 'pending',
        attempt_count = GREATEST(attempt_count, 1),
        container_image_identity_v2_authorized_status = CASE
            WHEN container_image_identity_v2_required THEN 'pending'
            ELSE ''
        END,
        container_image_identity_v3_authorized_status = CASE
            WHEN container_image_identity_v3_required THEN 'pending'
            ELSE ''
        END,
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = $1,
        next_attempt_at = NULL,
        failure_class = NULL,
        failure_message = NULL,
        failure_details = NULL,
        updated_at = $1,
        reopened_at = $1,
        last_requeue_reason = ` + queuestore.RequeueReasonStageGatedValue(
	telemetry.RequeueReasonRecoveryRedrive.SQLLiteral(), "") + `,
        ` + queuestore.ReplayRetryAllowanceAssignments + `
    WHERE work_item_id IN (SELECT work_item_id FROM candidates)
    RETURNING work_item_id
)
SELECT replayed.work_item_id, skipped.n
FROM skipped
LEFT JOIN replayed ON true
ORDER BY replayed.work_item_id
`
