// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// holderKeyRangeArm is one btree arm of the shipped holder read
// (FindSupplyChainImpactHoldersQuery) limited to a fact_id key range: the same
// literal fact kind, tombstone predicate, and payload expression, so it reads
// through the same partial lookup index, plus the range on fact_id, the
// index's next key column after impact_status (PostgreSQL 18 serves it with a
// skip scan over the few impact_status values). $1 lookup ids, $3 lower
// bound, $4 upper bound when bounded.
func holderKeyRangeArm(expr string, bounded bool) string {
	upper := ""
	if bounded {
		upper = "\n      AND finding.fact_id < $4"
	}
	return `
    SELECT finding.scope_id, finding.generation_id
    FROM fact_records AS finding
    WHERE finding.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
      AND finding.is_tombstone = FALSE
      AND finding.payload->>'` + expr + `' = ANY($1::text[])
      AND finding.fact_id >= $3` + upper
}

// holderActiveScopesTail joins a candidate (scope_id, generation_id) set to
// the scope's active generation and drops the asking scope, exactly like the
// shipped holder read. $2 is the asking scope.
const holderActiveScopesTail = `
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id
`

// FindSupplyChainImpactHolderKeyRangeQuery is the cve_id and advisory_id arms
// of the shipped holder read over one bounded fact_id key range [$3, $4)
// (#10643). A hot singleton id runs it once per range with
// FindSupplyChainImpactHolderOpenKeyRangeQuery for the last range and
// FindSupplyChainImpactMemberHoldersQuery for the member arm; on one snapshot
// their union is the shipped read. Exported for plan-shape and differential
// proofs.
var FindSupplyChainImpactHolderKeyRangeQuery = `
WITH candidates AS MATERIALIZED (` + holderKeyRangeArm("cve_id", true) + `
    UNION` + holderKeyRangeArm("advisory_id", true) + `
)` + holderActiveScopesTail

// FindSupplyChainImpactHolderOpenKeyRangeQuery is
// FindSupplyChainImpactHolderKeyRangeQuery for the last, unbounded-above range
// [$3, +inf). A separate text, not an "upper is empty OR" clause, keeps the
// lower bound an index condition under a generic plan.
var FindSupplyChainImpactHolderOpenKeyRangeQuery = `
WITH candidates AS MATERIALIZED (` + holderKeyRangeArm("cve_id", false) + `
    UNION` + holderKeyRangeArm("advisory_id", false) + `
)` + holderActiveScopesTail

// FindSupplyChainImpactMemberHoldersQuery is the advisory-group member arm of
// the shipped holder read alone, on its GIN partial index. A GIN bitmap has
// no key order to range over, so a hot id reads it as one statement.
const FindSupplyChainImpactMemberHoldersQuery = `
WITH candidates AS MATERIALIZED (
    SELECT finding.scope_id, finding.generation_id
    FROM fact_records AS finding
    WHERE finding.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
      AND finding.is_tombstone = FALSE
      AND finding.payload #> '{provenance,advisory_group_members}' ?| $1::text[]
)` + holderActiveScopesTail

// HolderKeyRangePlanCacheMode is the plan_cache_mode every key-range read
// runs under. A generic plan cannot see the fact_id bounds, so PostgreSQL
// 18.6 prices the range at its default selectivity and reads it through
// fact_records_pkey: a scan of a 1/16 slice of the whole table instead of a
// slice of one id's rows (TestHolderKeyRangeStatementPlanLive). A custom
// plan sees the bounds and keeps the partial lookup index with fact_id in
// its index condition.
const HolderKeyRangePlanCacheMode = "force_custom_plan"

// setHolderKeyRangePlanModeQuery scopes the plan mode to the read
// transaction, like the statement timeout.
const setHolderKeyRangePlanModeQuery = `SELECT set_config('plan_cache_mode', $1, true)`

// setHolderKeyRangeSnapshotQuery makes the key-range read one read-only
// REPEATABLE READ snapshot. It must be the transaction's first statement.
const setHolderKeyRangeSnapshotQuery = `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY`

// FindHolderScopesByKeyRange implements supplychaincore.HolderKeyRangeReader
// (#10643): the hot singleton's cve_id and advisory_id arms as one statement
// per key range, then its member arm, all in one read-only REPEATABLE READ
// transaction under HolderKeyRangePlanCacheMode. It runs on the primary (the
// reducer's own database), so the snapshot holds xmin for the whole read. One snapshot makes the union equal the shipped single
// statement on that snapshot, a generation flip mid-read included. Each
// statement runs under the same statement timeout as the holder read and
// fails with supplychaincore.ErrFanOutReadTimeout; the snapshot is held for
// at most len(ranges)+1 statement timeouts. The returned read lists every
// statement that ran, including a failed last one.
func (s SupplyChainImpactFanOutStore) FindHolderScopesByKeyRange(
	ctx context.Context,
	ownScopeID string,
	lookupIDs []string,
	ranges []supplychaincore.HolderKeyRange,
) (supplychaincore.HolderKeyRangeRead, error) {
	var read supplychaincore.HolderKeyRangeRead
	if len(lookupIDs) == 0 {
		return read, nil
	}
	if s.Beginner == nil {
		return read, errors.New("supply chain impact fan-out beginner is required")
	}
	timeout := s.ReadTimeout
	if timeout <= 0 {
		timeout = supplyChainImpactFanOutReadTimeout
	}
	tx, err := s.Beginner.Begin(ctx)
	if err != nil {
		return read, fmt.Errorf("begin supply chain impact holder key-range read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, setHolderKeyRangeSnapshotQuery); err != nil {
		return read, fmt.Errorf("set supply chain impact holder key-range snapshot: %w", err)
	}
	rows, err := tx.QueryContext(ctx, setSupplyChainImpactFanOutReadTimeoutQuery, fmt.Sprintf("%dms", timeout.Milliseconds()))
	if err != nil {
		return read, fmt.Errorf("set supply chain impact holder key-range timeout: %w", err)
	}
	_ = rows.Close()
	rows, err = tx.QueryContext(ctx, setHolderKeyRangePlanModeQuery, HolderKeyRangePlanCacheMode)
	if err != nil {
		return read, fmt.Errorf("set supply chain impact holder key-range plan mode: %w", err)
	}
	_ = rows.Close()
	seen := make(map[string]struct{})
	run := func(kind, query string, args ...any) error {
		start := time.Now()
		found, err := queryScopeIDs(ctx, tx, query, args...)
		read.Statements = append(read.Statements, supplychaincore.HolderReadStatement{Kind: kind, Duration: time.Since(start), Err: err})
		if err != nil {
			return err
		}
		for _, id := range found {
			seen[id] = struct{}{}
		}
		return nil
	}
	for i, keyRange := range ranges {
		var err error
		if keyRange.Upper == "" {
			err = run(telemetry.FanOutReadKindHoldersKeyRange, FindSupplyChainImpactHolderOpenKeyRangeQuery, lookupIDs, ownScopeID, keyRange.Lower)
		} else {
			err = run(telemetry.FanOutReadKindHoldersKeyRange, FindSupplyChainImpactHolderKeyRangeQuery, lookupIDs, ownScopeID, keyRange.Lower, keyRange.Upper)
		}
		if err != nil {
			// The kind and range index reach the dead letter's failure message,
			// so it names which statement timed out.
			return read, fmt.Errorf("%s statement %d of %d: %w", telemetry.FanOutReadKindHoldersKeyRange, i+1, len(ranges), err)
		}
	}
	if err := run(telemetry.FanOutReadKindHoldersMembers, FindSupplyChainImpactMemberHoldersQuery, lookupIDs, ownScopeID); err != nil {
		return read, fmt.Errorf("%s statement: %w", telemetry.FanOutReadKindHoldersMembers, err)
	}
	read.Scopes = make([]string, 0, len(seen))
	for id := range seen {
		read.Scopes = append(read.Scopes, id)
	}
	sort.Strings(read.Scopes)
	return read, nil
}

// queryScopeIDs runs one scope-id statement on tx and classifies a timeout
// like every fan-out read.
func queryScopeIDs(ctx context.Context, tx db.Transaction, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, classifyFanOutReadError(err)
	}
	defer func() { _ = rows.Close() }()
	var scopes []string
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return nil, fmt.Errorf("scan supply chain impact holder key-range scope: %w", err)
		}
		scopes = append(scopes, scopeID)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyFanOutReadError(err)
	}
	return scopes, nil
}

// The production store always satisfies the key-range seam: a decorator that
// stops forwarding it silently restores the unbounded hot singleton (#10643).
var _ supplychaincore.HolderKeyRangeReader = SupplyChainImpactFanOutStore{}
