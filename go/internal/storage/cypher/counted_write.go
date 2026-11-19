// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
)

// MatchedCountColumn is the RETURN alias every counted write template reports:
// the number of rows whose MATCH bound and whose MERGE committed (#10938).
// It is atomic with the write because it rides the same statement, so a
// MATCH-only row that binds nothing (first-generation absence, non-atomic
// re-projection clear) is counted instead of completing silently. The code-call
// edge templates report it; the runner reads it through this seam.
const MatchedCountColumn = "matched"

// CountedWriteExecutor is an optional capability an Executor implementation
// can add to report how many rows a mutating write statement matched. It exists
// for the code-call probe-to-write race (#10938): the edge templates MATCH
// both endpoints, and when the MATCH binds nothing the MERGE is a silent
// no-op while the worker completes the intent. The count closes the race at
// the write instead of with a pre-write probe, which always races.
//
// It is intentionally separate from CountProbeExecutor: that seam answers how
// many rows a READ-ONLY statement matches without mutating the graph, while
// this one reports the committed outcome of the write itself. Bolt write
// summary counters cannot carry it: RelationshipsCreated is 0 on an idempotent
// re-MERGE and PropertiesSet is shape-fragile, so neither distinguishes
// "matched" from "missed" on retry.
//
// The all-or-nothing forwarding rule for ProbeExecutor applies here too:
// every wrapper in this package that forwards GroupExecutor MUST forward both
// counted-write methods the same way, and the ExecuteOnly* wrappers MUST hide
// them, or a mid-chain gap silently downgrades an enforced code-call write to
// fail-closed with all tests green. A counted_write_follows_group test pins
// the column.
type CountedWriteExecutor interface {
	// ExecuteCountedWrite runs stmt (a mutating write reporting
	// MatchedCountColumn) and reports its matched count. count is meaningful
	// only when err is nil. Unlike ExecuteProbe it retries like a write:
	// callers have no cheap correct fallback, and a failed count leaves the
	// batch unverified, which fails the batch closed.
	ExecuteCountedWrite(ctx context.Context, stmt Statement) (int64, error)
}

// CountedWriteGroupExecutor is the grouped companion of CountedWriteExecutor:
// it runs stmts in one atomic transaction and reports one matched count per
// statement, aligned with stmts. A short slice or an error fails the whole
// group closed; callers never act on a partial count.
type CountedWriteGroupExecutor interface {
	// ExecuteCountedWriteGroup runs stmts atomically and reports the matched
	// count of each, in order. counts is meaningful only when err is nil and
	// has the same length as stmts.
	ExecuteCountedWriteGroup(ctx context.Context, stmts []Statement) ([]int64, error)
}

// ErrCountedWriteUnsupported is the shared sentinel every wrapper's
// counted-write fail-closed path reports (wrapping its own specific error)
// when the inner executor lacks the counted-write capability. The code-call
// writer maps it to a fail-closed counting error: an unverified batch never
// completes silently.
var ErrCountedWriteUnsupported = errors.New("executor chain does not support counted writes")

// CountedWriteCounts validates the counts a counted-write group execution
// returned: same length as stmts, no negatives. It fails closed so callers
// never act on a short or negative count as if it were a verified write.
func CountedWriteCounts(stmts []Statement, counts []int64) error {
	if len(counts) != len(stmts) {
		return errors.New("counted write group returned a short count slice")
	}
	for _, count := range counts {
		if count < 0 {
			return errors.New("counted write group returned a negative count")
		}
	}
	return nil
}
