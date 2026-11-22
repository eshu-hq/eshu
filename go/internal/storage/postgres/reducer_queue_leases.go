// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Lease-mirror CTE fragments for #10879 phase 1 (dual-write cutover).
//
// Every reducer writer that creates or extends liveness maintains the
// fact_work_item_leases mirror in the same statement, with the main row
// locked first and the lease row second (the global main-then-lease lock
// order). Phase 1 keeps the main columns authoritative: no reader changes,
// so behavior is unchanged and the mirror can be verified row-for-row
// before phase 2 switches reads and narrows heartbeat.
//
// A data-modifying CTE executes exactly once even when the outer query does
// not read its output, so the terminal chains below append the clear as an
// unreferenced CTE without changing their outer SELECT shape (and therefore
// their RowsAffected contract under ExecContext).

// queryTerminalMatchCount runs a phase-1 terminal statement (a WITH
// done/cleared chain ending in SELECT (SELECT count(*) FROM done)) and
// reports the main-table match count. Success depends only on the fence
// matching, never on mirror presence, so hand-inserted live rows and
// version-skewed writers terminate exactly like claimed ones; the mirror
// delete stays in-statement and atomic.
func queryTerminalMatchCount(
	ctx context.Context,
	database db.ExecQueryer,
	op string,
	query string,
	args ...any,
) (int64, error) {
	rows, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	defer func() { _ = rows.Close() }()
	var matched int64
	if rows.Next() {
		if err := rows.Scan(&matched); err != nil {
			return 0, fmt.Errorf("%s: scan match count: %w", op, err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	return matched, nil
}

// Lease liveness (#11081 shared definition, used verbatim in the claim
// CTEs, the heartbeat, migration 326, and the orphan gauge): lease row L
// (stage='reducer') is LIVE iff EXISTS main row M with
// M.work_item_id=L.work_item_id AND M.stage='reducer' AND M.status IN
// ('claimed','running') AND M.claim_until>NOW AND
// M.lease_owner=L.lease_owner AND M.last_attempt_at=L.last_attempt_at.
// STALE = NOT LIVE. NOW is the claim's $1 (the app clock, same as the
// candidate fences). Never judge from L.claim_until: an old-build heartbeat
// extends main only, so mirror expiry lags under version skew.
//
// Hold-end census (#11081 decision 3b): a statement is a hold-end iff its
// WHERE can match a reducer row in claimed/running (including expired) or
// null its fencing triple. Verified NOT hold-ends (no edit): retouch
// (reducer_queue_retouch.go preserves claimed/running), reopen/replay
// succeeded composers (reducer_queue_replay.go match status='succeeded'
// only), waiting wakes (reducer_waiting_store.go,
// reducer_waiting_winners_wake.go, relationship_store_resolved.go touch
// succeeded/terminal sets only), supersede-inactive sweep
// (reducer_generation_filter_sql.go:138 unleased-only predicate),
// generation-liveness wedged recovery (generation_liveness_sql.go re-drives
// projector-stage rows only; the lease mirror holds reducer rows). Full
// census extension (#11081 executor pass, every UPDATE fact_work_items and
// lease_owner=NULL writer read): poison recovery matches dead_letter only;
// every projector_* writer is stage='projector' or a synthetic projector-id
// upsert (reproject, zombie heal, refinalize likewise); semantic/,
// activation/, scope/completion, and shared_intents lease columns belong to
// other tables; the CASE-preserve reopeners (retouch, supply-chain fanout,
// service-catalog retrigger, completion fanout) null the triple only for
// succeeded rows, or for dead_letter rows too (retouch and, since #11147, the
// supply-chain fanout, which also reopens dead letters; failed for retouch),
// none of which is a hold, and keep claimed/running holds intact; the remaining
// claimed/running writers stamp payloads or flags only (fanout
// clear/inflight, manifest stamp, completion-wake claimed arm, identity
// cutover epoch and claimed-to-running marks). The fence
// mark is the only confirmed hold-end gap and carries its own same-statement
// clear. The scan's latent
// class needs no edits: same-item residue heals via the PK upsert, and any
// sibling residue heals here at claim time.

// reducerQueueLeaseMirrorCTE inserts (or refreshes, on reclaim) the mirror
// row for the just-claimed rows. It is appended after the claimed CTE of a
// claim statement, which supplies $1 (now, the liveness clock), $3 (owner),
// $4 (claim_until) and the per-row claimed_at (the new last_attempt_at).
// The chained stale_leases_cleared CTE first deletes, on each just-claimed
// conflict key, every sibling lease row the main table disowns (a leftover
// from a previous-build writer, the retire fence, or the 323 backfill
// race), so the mirror insert cannot wedge on an orphaned sibling row.
// Live sibling rows (a genuinely live holder under version skew) are
// preserved by the NOT EXISTS guard and still defer through the main unique
// index, which fires first. The join back to fact_work_items carries the
// denormalized conflict columns; those rows are already locked by the claim
// UPDATE, so the join cannot block. Lock order stays main-then-lease: the
// claimed mains are locked by the claim UPDATE and the clear locks only the
// lease rows it deletes, while live holders are read unlocked.
const reducerQueueLeaseMirrorCTE = `stale_leases_cleared AS (
    DELETE FROM fact_work_item_leases AS lease
    USING claimed
    JOIN fact_work_items AS claimed_work
        ON claimed_work.work_item_id = claimed.work_item_id
    WHERE lease.stage = 'reducer'
      AND lease.conflict_domain = claimed_work.conflict_domain
      AND COALESCE(lease.conflict_key, lease.scope_id)
          = COALESCE(claimed_work.conflict_key, claimed_work.scope_id)
      AND lease.work_item_id <> claimed.work_item_id
      AND NOT EXISTS (
          SELECT 1 FROM fact_work_items AS holder
          WHERE holder.work_item_id = lease.work_item_id
            AND holder.stage = 'reducer'
            AND holder.status IN ('claimed', 'running')
            AND holder.claim_until > $1
            AND holder.lease_owner = lease.lease_owner
            AND holder.last_attempt_at = lease.last_attempt_at
      )
    RETURNING lease.work_item_id
),
leased AS (
    INSERT INTO fact_work_item_leases (
        work_item_id, stage, scope_id, conflict_domain, conflict_key,
        lease_owner, claim_until, last_attempt_at
    )
    SELECT claimed.work_item_id, 'reducer', claimed.scope_id,
        work.conflict_domain, work.conflict_key,
        $3, $4, claimed.claimed_at
    FROM claimed
    JOIN fact_work_items AS work ON work.work_item_id = claimed.work_item_id
    WHERE (SELECT count(*) FROM stale_leases_cleared) >= 0
    ON CONFLICT (work_item_id) DO UPDATE SET
        scope_id = EXCLUDED.scope_id,
        conflict_domain = EXCLUDED.conflict_domain,
        conflict_key = EXCLUDED.conflict_key,
        lease_owner = EXCLUDED.lease_owner,
        claim_until = EXCLUDED.claim_until,
        last_attempt_at = EXCLUDED.last_attempt_at
)`

// reducerLiveLeaseConflictConstraint reports whether err is the unique-index
// violation that fences a second concurrent live lease on a conflict key
// (#4137, completing #3558), and if so which constraint rejected the claim:
// "main" for the main-table live-lease fence
// (fact_work_items_reducer_live_lease_uniq, genuine contention) or "mirror"
// for its lease-mirror twin (fact_work_item_leases_live_lease_uniq,
// leftover residue or a same-key race through version-skewed writers).
// The constraint name is matched explicitly so an unrelated unique
// violation is never swallowed.
func reducerLiveLeaseConflictConstraint(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return "", false
	}
	switch pgErr.ConstraintName {
	case reducerLiveLeaseUniqueConstraint:
		return telemetry.LiveLeaseConflictConstraintMain, true
	case reducerLeaseMirrorUniqueConstraint:
		return telemetry.LiveLeaseConflictConstraintMirror, true
	default:
		return "", false
	}
}

// noteReducerLiveLeaseConflict records one claim deferred by a live lease
// conflict (#11081 AC2): the conflict counter (closed claim_mode +
// constraint labels, nil-safe) and one Warn line through the queue's logger
// (slog.Default fallback). Only the queue's own owner travels in the log;
// conflict keys, scopes, and work item ids stay out of the labels and attrs.
func (q ReducerQueue) noteReducerLiveLeaseConflict(ctx context.Context, claimMode string, err error) {
	constraint, ok := reducerLiveLeaseConflictConstraint(err)
	if !ok {
		return
	}
	if q.Instruments != nil && q.Instruments.ReducerClaimLiveLeaseConflicts != nil {
		q.Instruments.ReducerClaimLiveLeaseConflicts.Add(ctx, 1,
			metric.WithAttributes(
				telemetry.AttrClaimMode(claimMode),
				telemetry.AttrConstraint(constraint),
			))
	}
	logger := q.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.WarnContext(ctx, "reducer claim deferred by live lease conflict",
		slog.String("claim_mode", claimMode),
		slog.String("constraint", constraint),
		slog.String("lease_owner", q.LeaseOwner),
		slog.String("error", err.Error()),
	)
}

// reducerQueueLeaseClearCTE deletes the mirror rows for a terminal
// statement's acknowledged CTE. Every terminal chain names that CTE
// acknowledged and returns work_item_id from it.
const reducerQueueLeaseClearCTE = `cleared_leases AS (
    DELETE FROM fact_work_item_leases AS lease
    USING acknowledged
    WHERE lease.work_item_id = acknowledged.work_item_id
)`

// heartbeatReducerWorkQuery extends the hold on one claimed reducer work
// item. Phase 1 of #10879 dual-writes the fencing triple: the main row keeps
// its authoritative columns and the lease mirror is upserted in the same
// statement (main row first, then the lease row, matching the global
// main-then-lease lock order). RowsAffected stays 1 iff the fence matched:
// an empty hb inserts nothing. A missing mirror row (unreachable through the
// wired writers) is recreated here, so the heartbeat self-heals the mirror.
// The chained stale_leases_cleared CTE (#11081) first deletes, on the
// heartbeat row's conflict key, every sibling lease row the main table
// disowns — same shared liveness definition as the claim, keyed on the hb
// row with NOW=$2 — so a sibling leftover cannot abort the heartbeat's
// mirror recreate with a raw unique violation.
const heartbeatReducerWorkQuery = `
WITH hb AS (
    UPDATE fact_work_items
    SET claim_until = $1,
        updated_at = $2
    WHERE work_item_id = $3
      AND stage = 'reducer'
      AND lease_owner = $4
      AND status IN ('claimed', 'running')
      AND claim_until > clock_timestamp()
      AND last_attempt_at = $5
    RETURNING work_item_id, lease_owner, claim_until, last_attempt_at
),
stale_leases_cleared AS (
    DELETE FROM fact_work_item_leases AS lease
    USING hb
    JOIN fact_work_items AS hb_work ON hb_work.work_item_id = hb.work_item_id
    WHERE lease.stage = 'reducer'
      AND lease.conflict_domain = hb_work.conflict_domain
      AND COALESCE(lease.conflict_key, lease.scope_id)
          = COALESCE(hb_work.conflict_key, hb_work.scope_id)
      AND lease.work_item_id <> hb.work_item_id
      AND NOT EXISTS (
          SELECT 1 FROM fact_work_items AS holder
          WHERE holder.work_item_id = lease.work_item_id
            AND holder.stage = 'reducer'
            AND holder.status IN ('claimed', 'running')
            AND holder.claim_until > $2
            AND holder.lease_owner = lease.lease_owner
            AND holder.last_attempt_at = lease.last_attempt_at
      )
    RETURNING lease.work_item_id
)
INSERT INTO fact_work_item_leases (
    work_item_id, stage, scope_id, conflict_domain, conflict_key,
    lease_owner, claim_until, last_attempt_at
)
SELECT hb.work_item_id, 'reducer', work.scope_id, work.conflict_domain,
    work.conflict_key, hb.lease_owner, hb.claim_until, hb.last_attempt_at
FROM hb
JOIN fact_work_items AS work ON work.work_item_id = hb.work_item_id
WHERE (SELECT count(*) FROM stale_leases_cleared) >= 0
ON CONFLICT (work_item_id) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    conflict_domain = EXCLUDED.conflict_domain,
    conflict_key = EXCLUDED.conflict_key,
    lease_owner = EXCLUDED.lease_owner,
    claim_until = EXCLUDED.claim_until,
    last_attempt_at = EXCLUDED.last_attempt_at
`
