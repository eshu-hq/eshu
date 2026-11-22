// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// fanOutHolderKeyRangeCount is how many fact_id key ranges a hot singleton
// holder read runs as (#10643). The shipped singleton read of the hottest QA
// id (83,642 cve-arm rows) spent 7.7 s of I/O against the 10 s guard; 16
// ranges put about 5.2k rows in each statement, which the QA replica read
// cold in about 0.5-1 s on the same lookup index. Total work is unchanged:
// each row is still fetched once, by exactly one range.
const fanOutHolderKeyRangeCount = 16

// holderKeyRangeHexDigits are the split points of the key ranges. A finding
// fact_id is the fact kind, a colon, and a hex digest, so its first digest
// digit is uniform and 16 ranges split a hot id's rows evenly. Coverage does
// not depend on that format: the ranges partition the whole text key space,
// so any fact_id falls in exactly one range; only the balance does.
const holderKeyRangeHexDigits = "0123456789abcdef"

// HolderKeyRange is one half-open fact_id key range [Lower, Upper) of a
// ranged holder read. An empty Lower is unbounded below and an empty Upper is
// unbounded above.
type HolderKeyRange struct {
	Lower string
	Upper string
}

// HolderKeyRanges returns the fanOutHolderKeyRangeCount ranges a hot singleton
// holder read runs as, in key order. Together they partition the key space:
// the first is unbounded below, the last unbounded above, and each upper
// bound is the next range's lower bound.
func HolderKeyRanges() []HolderKeyRange {
	prefix := facts.ReducerSupplyChainImpactFindingFactKind + ":"
	step := len(holderKeyRangeHexDigits) / fanOutHolderKeyRangeCount
	ranges := make([]HolderKeyRange, 0, fanOutHolderKeyRangeCount)
	lower := ""
	for i := step; i < len(holderKeyRangeHexDigits); i += step {
		upper := prefix + holderKeyRangeHexDigits[i:i+1]
		ranges = append(ranges, HolderKeyRange{Lower: lower, Upper: upper})
		lower = upper
	}
	return append(ranges, HolderKeyRange{Lower: lower})
}

// HolderReadStatement is one statement of a key-range holder read: its read
// kind (telemetry.FanOutReadKindHoldersKeyRange), how long it ran, and its
// error, if any. The handler records each into the fan-out read-duration histogram.
type HolderReadStatement struct {
	Kind     string
	Duration time.Duration
	Err      error
}

// HolderKeyRangeRead is the result of one key-range holder read: the holder
// scopes and every statement that ran, in order, including a failed last one.
type HolderKeyRangeRead struct {
	Scopes     []string
	Statements []HolderReadStatement
}

// HolderKeyRangeReader is the optional seam behind bounded hot-id holder reads
// (#10643). A hot singleton id, one whose measured mass is over
// fanOutHolderHotMassRows, reads as one statement per HolderKeyRanges range.
// Each statement carries the cve_id and advisory_id arms over its fact_id
// range and the advisory-group member arm as a foreign-member token arm over
// its share of the token buckets (#10643 PR 2), so no statement reads all of
// the id's rows from any arm. All of them run in one read-only REPEATABLE READ
// transaction, so they share one snapshot and their union equals
// FindHolderScopes for that id on that snapshot, a generation flip mid-read
// included. Each statement keeps the 10 s holder-read guard and the typed
// timeout error. A port without the seam keeps the shipped singleton
// statement.
type HolderKeyRangeReader interface {
	// FindHolderScopesByKeyRange returns the scopes, other than ownScopeID,
	// holding an active finding row for lookupIDs, read as one statement per
	// range on one snapshot. On error the
	// returned read still lists the statements that ran.
	FindHolderScopesByKeyRange(ctx context.Context, ownScopeID string, lookupIDs []string, ranges []HolderKeyRange) (HolderKeyRangeRead, error)
}
