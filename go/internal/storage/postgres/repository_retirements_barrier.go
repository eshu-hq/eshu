// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// BlockingLease names the oldest lease holding the retirement barrier, so
// an operator sees which partition to look at while 2b' waits.
type BlockingLease struct {
	// Domain is the projection_domain holding the lease.
	Domain string
	// PartitionID is the held partition.
	PartitionID int
	// LeaseOwner is the process-unique worker holding the lease.
	LeaseOwner string
	// ClaimedAt is the lease's claim epoch; nil for pre-migration rows.
	ClaimedAt *time.Time
}

// RetirementBarrierStore owns the 2b/2b' intent-delete barrier (#8659).
// Step 2b deletes the repository's shared intents, then stamps
// intent_barrier_at in a later statement so the stamp postdates the
// delete's commit. Step 2b' waits until no live lease claimed at or before
// that barrier remains: every edge written from a pre-barrier read lands
// before the barrier passes, and 2c retracts it.
type RetirementBarrierStore struct {
	database db.ExecQueryer
}

// NewRetirementBarrierStore binds the barrier queries to the database.
func NewRetirementBarrierStore(database db.ExecQueryer) RetirementBarrierStore {
	return RetirementBarrierStore{database: database}
}

// retirementStampBarrierSQL stamps the barrier after the 2b delete commits.
// A re-run of 2b that deletes rows stamps again the same way, so the
// barrier only ever moves later and a re-run 2b' waits for every lease
// claimed before the new delete.
const retirementStampBarrierSQL = `UPDATE repository_retirements
	SET intent_barrier_at = clock_timestamp(), updated_at = clock_timestamp()
	WHERE retirement_id = $1 AND readmitted_at IS NULL
	RETURNING intent_barrier_at`

// StampIntentBarrier stamps intent_barrier_at on the tombstone and returns
// the stamped barrier. Zero affected rows means the tombstone is gone or
// readmitted, and the retirement must not proceed to the graph phase.
func (s RetirementBarrierStore) StampIntentBarrier(ctx context.Context, retirementID string) (time.Time, error) {
	rows, err := s.database.QueryContext(ctx, retirementStampBarrierSQL, retirementID)
	if err != nil {
		return time.Time{}, fmt.Errorf("retirement stamp barrier: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return time.Time{}, fmt.Errorf("retirement stamp barrier: %w", err)
		}
		return time.Time{}, fmt.Errorf("retirement stamp barrier %s: no open tombstone row", retirementID)
	}
	var stamped sql.NullTime
	if err := rows.Scan(&stamped); err != nil {
		return time.Time{}, fmt.Errorf("retirement stamp barrier scan: %w", err)
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, fmt.Errorf("retirement stamp barrier: %w", err)
	}
	if !stamped.Valid {
		return time.Time{}, fmt.Errorf("retirement stamp barrier %s: barrier is NULL after stamp", retirementID)
	}
	return stamped.Time.UTC(), nil
}

// retirementPreBarrierLeaseCountSQL is the 2b' wait predicate: live leases
// (held past now) in the listed domains whose claim epoch predates the
// barrier. Rows from before the claimed_at migration read as NULL and
// block until their first re-claim, which is one worker cycle. The read is
// fleet-wide across the listed domains because nothing records which
// partition holds this repository's intents.
const retirementPreBarrierLeaseCountSQL = `SELECT count(*) FROM shared_projection_partition_leases
	WHERE lease_owner IS NOT NULL AND lease_expires_at > clock_timestamp()
	  AND projection_domain = ANY($1)
	  AND (claimed_at IS NULL OR claimed_at <= $2)`

// retirementOldestBlockingLeaseSQL names the oldest blocking lease for the
// 2b' readback. NULL epochs sort first: a pre-migration row is the one the
// operator most needs to see.
const retirementOldestBlockingLeaseSQL = `SELECT projection_domain, partition_id, lease_owner, claimed_at
	FROM shared_projection_partition_leases
	WHERE lease_owner IS NOT NULL AND lease_expires_at > clock_timestamp()
	  AND projection_domain = ANY($1)
	  AND (claimed_at IS NULL OR claimed_at <= $2)
	ORDER BY claimed_at NULLS FIRST, projection_domain, partition_id
	LIMIT 1`

// ScopeQuiesceCounts is the 2q recheck result: every count must be zero
// for the graph retraction to run.
type ScopeQuiesceCounts struct {
	// LiveGenerations are scope generations still in a non-terminal
	// status (pending, active, failed).
	LiveGenerations int64
	// ActionableReducerRows are reducer rows that could still write:
	// pending, retrying, failed, dead-letter, or claimed/running past
	// their claim (the fence 7d predicate).
	ActionableReducerRows int64
	// LiveReducerLeases are claimed/running reducer rows holding a live
	// claim: a handler is inside its write window right now.
	LiveReducerLeases int64
	// LiveProjectorLeases are claimed/running projector rows holding a
	// live claim.
	LiveProjectorLeases int64
}

// Quiet reports whether every count is zero.
func (c ScopeQuiesceCounts) Quiet() bool {
	return c.LiveGenerations == 0 && c.ActionableReducerRows == 0 &&
		c.LiveReducerLeases == 0 && c.LiveProjectorLeases == 0
}

// 2q recheck predicates. They repeat the fence's status predicates
// (repository_retirements_fence_sql.go) so the recheck and the fence can
// never disagree on what "quiesced" means; only the keying differs (the
// tombstone's captured scope ids instead of the request's repo ids). The
// hermetic pin test below fails when either side changes a status list.
const (
	retirementRecheckGenerationsSQL = `SELECT count(*) FROM scope_generations
		WHERE scope_id = ANY($1) AND status IN ('pending', 'active', 'failed')`
	retirementRecheckReducerRowsSQL = `SELECT count(*) FROM fact_work_items
		WHERE scope_id = ANY($1) AND stage = 'reducer'
		AND (status IN ('pending', 'retrying', 'failed', 'dead_letter')
			OR (status IN ('claimed', 'running') AND claim_until <= clock_timestamp()))`
	retirementRecheckLiveReducerLeasesSQL = `SELECT count(*) FROM fact_work_items
		WHERE scope_id = ANY($1) AND stage = 'reducer'
		AND status IN ('claimed', 'running') AND claim_until > clock_timestamp()`
	retirementRecheckLiveProjectorLeasesSQL = `SELECT count(*) FROM fact_work_items
		WHERE scope_id = ANY($1) AND stage = 'projector'
		AND status IN ('claimed', 'running') AND claim_until > clock_timestamp()`
)

// ReassertScopesQuiesced re-runs the fence predicates over the tombstone's
// captured scope ids just before the graph retraction (step 2q). The fence
// quiesced these scopes minutes ago; a reopened row, a refinalized scope,
// or a maintenance pass can undo that while nobody holds a lock. A
// non-quiet result fails the retirement closed: the runner refuses and
// remarks the tombstone instead of retracting under live writers, so no
// reducer row can reopen and write graph edges after the retraction.
func ReassertScopesQuiesced(ctx context.Context, database db.ExecQueryer, scopeIDs []string) (ScopeQuiesceCounts, error) {
	var counts ScopeQuiesceCounts
	queries := []struct {
		text  string
		apply func(int64)
	}{
		{retirementRecheckGenerationsSQL, func(n int64) { counts.LiveGenerations = n }},
		{retirementRecheckReducerRowsSQL, func(n int64) { counts.ActionableReducerRows = n }},
		{retirementRecheckLiveReducerLeasesSQL, func(n int64) { counts.LiveReducerLeases = n }},
		{retirementRecheckLiveProjectorLeasesSQL, func(n int64) { counts.LiveProjectorLeases = n }},
	}
	for _, query := range queries {
		rows, err := database.QueryContext(ctx, query.text, scopeIDs)
		if err != nil {
			return ScopeQuiesceCounts{}, fmt.Errorf("retirement reassert quiesced: %w", err)
		}
		var count int64
		if rows.Next() {
			if err := rows.Scan(&count); err != nil {
				_ = rows.Close()
				return ScopeQuiesceCounts{}, fmt.Errorf("retirement reassert quiesced scan: %w", err)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return ScopeQuiesceCounts{}, fmt.Errorf("retirement reassert quiesced: %w", err)
		}
		_ = rows.Close()
		query.apply(count)
	}
	return counts, nil
}

// CountPreBarrierLeases returns the number of live leases claimed at or
// before the barrier, with the oldest one named for the operator readback.
// Zero means 2b' passes and the graph retraction may run.
func (s RetirementBarrierStore) CountPreBarrierLeases(
	ctx context.Context,
	domains []string,
	barrier time.Time,
) (int64, BlockingLease, error) {
	rows, err := s.database.QueryContext(ctx, retirementPreBarrierLeaseCountSQL, domains, barrier.UTC())
	if err != nil {
		return 0, BlockingLease{}, fmt.Errorf("retirement pre-barrier lease count: %w", err)
	}
	var count int64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			_ = rows.Close()
			return 0, BlockingLease{}, fmt.Errorf("retirement pre-barrier lease count scan: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, BlockingLease{}, fmt.Errorf("retirement pre-barrier lease count: %w", err)
	}
	_ = rows.Close()
	if count == 0 {
		return 0, BlockingLease{}, nil
	}
	oldestRows, err := s.database.QueryContext(ctx, retirementOldestBlockingLeaseSQL, domains, barrier.UTC())
	if err != nil {
		return 0, BlockingLease{}, fmt.Errorf("retirement oldest blocking lease: %w", err)
	}
	defer func() { _ = oldestRows.Close() }()
	var oldest BlockingLease
	var claimedAt sql.NullTime
	if oldestRows.Next() {
		if err := oldestRows.Scan(&oldest.Domain, &oldest.PartitionID, &oldest.LeaseOwner, &claimedAt); err != nil {
			return 0, BlockingLease{}, fmt.Errorf("retirement oldest blocking lease scan: %w", err)
		}
		if claimedAt.Valid {
			claimed := claimedAt.Time.UTC()
			oldest.ClaimedAt = &claimed
		}
	}
	if err := oldestRows.Err(); err != nil {
		return 0, BlockingLease{}, fmt.Errorf("retirement oldest blocking lease: %w", err)
	}
	return count, oldest, nil
}
