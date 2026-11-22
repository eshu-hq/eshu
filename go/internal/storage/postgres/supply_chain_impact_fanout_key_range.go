// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// holderKeyRangeArm is one btree arm of the shipped holder read
// (FindSupplyChainImpactHoldersQuery) limited to a fact_id key range: the same
// literal fact kind, tombstone predicate, and payload expression, so it reads
// through the same partial indexes, plus the range on fact_id, the
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

// holderMemberTokenArm is the member arm of the shipped holder read as a
// foreign-member token arm (#10643 PR 2): the finding rows whose token
// function output overlaps the statement's lookup tokens, through
// SupplyChainImpactForeignMemberTokensIndex. It has no fact_id bound: the
// token's bucket, not the key range, splits the foreign rows over the
// statements (HolderMemberTokens), and the union over all statements on one
// snapshot is what makes the read exact. Own-key rows never carry a token;
// the cve_id and advisory_id arms return them. param is the tokens
// parameter.
func holderMemberTokenArm(param string) string {
	return `
    SELECT finding.scope_id, finding.generation_id
    FROM fact_records AS finding
    WHERE finding.fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'
      AND finding.is_tombstone = FALSE
      AND ` + SupplyChainImpactForeignMemberTokensFunction + `(finding.fact_id, finding.payload) && ` + param + `::text[]`
}

// holderMemberTokenAlphabet is the bucket alphabet of the token function:
// strpos over it of a fact id's last character is the bucket, 1 to 16 for
// these characters and 0 for any other. It must stay equal to the literal in
// supplyChainImpactForeignMemberTokensFunctionSQL.
const holderMemberTokenAlphabet = "0123456789abcdef"

// HolderMemberTokenMaxBytes is the longest string, in bytes, the token
// function turns into a token (#10643 PR 2, arbiter ruling 10643m). A GIN
// array_ops entry cannot exceed 2,712 bytes, so a longer member would fail
// the finding insert; the function leaves it untokenized instead, and a read
// whose lookup ids include a longer one uses the shipped statement. It must
// stay equal to the literal in supplyChainImpactForeignMemberTokensFunctionSQL.
const HolderMemberTokenMaxBytes = 1024

// HolderLookupExceedsTokenBytes reports whether any of lookupIDs is longer
// than HolderMemberTokenMaxBytes bytes, so no token can match it.
func HolderLookupExceedsTokenBytes(lookupIDs []string) bool {
	for _, id := range lookupIDs {
		if len(id) > HolderMemberTokenMaxBytes {
			return true
		}
	}
	return false
}

// HolderMemberTokens returns the foreign-member lookup tokens statement
// (0-based) of a ranged read of statements statements carries for
// lookupIDs: bucket || ":" || id for each id and each bucket the statement
// owns. Bucket b in 1..16 belongs to statement (b-1) mod statements, and
// bucket 0 to statement 0, so over the statements every (bucket, id) pair is
// carried exactly once. With the shipped 16 ranges, statement 0 carries
// buckets 0 and 1 and statement k carries bucket k+1. It returns nil when
// lookupIDs is empty or statements is not positive.
func HolderMemberTokens(lookupIDs []string, statement, statements int) []string {
	if len(lookupIDs) == 0 || statements <= 0 {
		return nil
	}
	var tokens []string
	for bucket := 0; bucket <= len(holderMemberTokenAlphabet); bucket++ {
		owner := 0
		if bucket > 0 {
			owner = (bucket - 1) % statements
		}
		if owner != statement {
			continue
		}
		prefix := strconv.Itoa(bucket) + ":"
		for _, id := range lookupIDs {
			tokens = append(tokens, prefix+id)
		}
	}
	return tokens
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
// of the shipped holder read over one bounded fact_id key range [$3, $4),
// plus its member arm as the foreign-member token arm on lookup tokens $5
// (#10643). A hot singleton id runs it once per range with
// FindSupplyChainImpactHolderOpenKeyRangeQuery for the last range; on one
// snapshot their union is the shipped read. Exported for plan-shape and
// differential proofs.
var FindSupplyChainImpactHolderKeyRangeQuery = `
WITH candidates AS MATERIALIZED (` + holderKeyRangeArm("cve_id", true) + `
    UNION` + holderKeyRangeArm("advisory_id", true) + `
    UNION` + holderMemberTokenArm("$5") + `
)` + holderActiveScopesTail

// FindSupplyChainImpactHolderOpenKeyRangeQuery is
// FindSupplyChainImpactHolderKeyRangeQuery for the last, unbounded-above range
// [$3, +inf), its lookup tokens in $4. A separate text, not an "upper is
// empty OR" clause, keeps the lower bound an index condition under a generic
// plan.
var FindSupplyChainImpactHolderOpenKeyRangeQuery = `
WITH candidates AS MATERIALIZED (` + holderKeyRangeArm("cve_id", false) + `
    UNION` + holderKeyRangeArm("advisory_id", false) + `
    UNION` + holderMemberTokenArm("$4") + `
)` + holderActiveScopesTail

// HolderKeyRangePlanCacheMode is the plan_cache_mode every key-range read
// runs under. A generic plan cannot see the fact_id bounds, so PostgreSQL
// 18.6 prices the range at its default selectivity and reads it through
// fact_records_pkey: a scan of a 1/16 slice of the whole table instead of a
// slice of one id's rows (TestHolderKeyRangeStatementPlanLive). A custom
// plan sees the bounds and reads each arm's own lookup index with its lookup
// expression and both fact_id bounds in the Index Cond.
const HolderKeyRangePlanCacheMode = "force_custom_plan"

// setHolderKeyRangePlanModeQuery scopes the plan mode to the read
// transaction, like the statement timeout.
const setHolderKeyRangePlanModeQuery = `SELECT set_config('plan_cache_mode', $1, true)`

// setHolderKeyRangeSnapshotQuery makes the key-range read one read-only
// REPEATABLE READ snapshot. It must be the transaction's first statement.
const setHolderKeyRangeSnapshotQuery = `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY`

// FindHolderScopesByKeyRange implements supplychaincore.HolderKeyRangeReader
// (#10643): the hot singleton's cve_id, advisory_id and foreign-member token
// arms as one statement per key range, all in one read-only REPEATABLE READ
// transaction under HolderKeyRangePlanCacheMode. Statement i carries the
// lookup tokens HolderMemberTokens(lookupIDs, i, len(ranges)), so the token
// arms together read every foreign-member row once. It runs on the primary (the
// reducer's own database), so the snapshot holds xmin for the whole read. One snapshot makes the union equal the shipped single
// statement on that snapshot, a generation flip mid-read included. Each
// statement runs under the same statement timeout as the holder read and
// fails with supplychaincore.ErrFanOutReadTimeout; the snapshot is held for
// at most len(ranges) statement timeouts. An empty ranges is an error: no
// statement would read the member arm. The returned read lists every
// statement that ran, including a failed last one.
//
// A lookup set holding an id over HolderMemberTokenMaxBytes bytes cannot be
// read through tokens: the token function never stores one for it (arbiter
// ruling 10643m). The whole read then runs the shipped holder statement
// (FindSupplyChainImpactHoldersQuery, kind telemetry.FanOutReadKindHolders,
// range 1) in the same transaction, builds no token, and sets read.Fallback.
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
	if len(ranges) == 0 {
		return read, errors.New("supply chain impact holder key-range read needs at least one key range")
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
	seen := make(map[string]struct{})
	run := func(rangeIndex int, kind, query string, args ...any) error {
		start := time.Now()
		found, err := queryScopeIDs(ctx, tx, query, args...)
		read.Statements = append(read.Statements, supplychaincore.HolderReadStatement{Kind: kind, Range: rangeIndex, Duration: time.Since(start), Err: err})
		if err != nil {
			return err
		}
		for _, id := range found {
			seen[id] = struct{}{}
		}
		return nil
	}
	if HolderLookupExceedsTokenBytes(lookupIDs) {
		read.Fallback = true
		if err := run(1, telemetry.FanOutReadKindHolders, FindSupplyChainImpactHoldersQuery, lookupIDs, ownScopeID); err != nil {
			return read, fmt.Errorf("%s fallback statement for a lookup id over %d bytes: %w", telemetry.FanOutReadKindHolders, HolderMemberTokenMaxBytes, err)
		}
		read.Scopes = sortedScopes(seen)
		return read, nil
	}
	rows, err = tx.QueryContext(ctx, setHolderKeyRangePlanModeQuery, HolderKeyRangePlanCacheMode)
	if err != nil {
		return read, fmt.Errorf("set supply chain impact holder key-range plan mode: %w", err)
	}
	_ = rows.Close()
	for i, keyRange := range ranges {
		var err error
		tokens := HolderMemberTokens(lookupIDs, i, len(ranges))
		if keyRange.Upper == "" {
			err = run(i+1, telemetry.FanOutReadKindHoldersKeyRange, FindSupplyChainImpactHolderOpenKeyRangeQuery, lookupIDs, ownScopeID, keyRange.Lower, tokens)
		} else {
			err = run(i+1, telemetry.FanOutReadKindHoldersKeyRange, FindSupplyChainImpactHolderKeyRangeQuery, lookupIDs, ownScopeID, keyRange.Lower, keyRange.Upper, tokens)
		}
		if err != nil {
			// The kind and range index reach the dead letter's failure message,
			// so it names which statement timed out.
			return read, fmt.Errorf("%s statement %d of %d: %w", telemetry.FanOutReadKindHoldersKeyRange, i+1, len(ranges), err)
		}
	}
	read.Scopes = sortedScopes(seen)
	return read, nil
}

// sortedScopes returns the scope ids of seen in order.
func sortedScopes(seen map[string]struct{}) []string {
	scopes := make([]string, 0, len(seen))
	for id := range seen {
		scopes = append(scopes, id)
	}
	sort.Strings(scopes)
	return scopes
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
