// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"strings"
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

// HolderKeyRangeMassGate is the true per-id row mass above which a hot
// singleton holder id reads as fact_id key-range statements (#10972 H2 A2).
// The H2 shim proved the shipped singleton at a warm ~1.3 s isolated on the
// hottest 156,870-row advisory ids but p95 3.45 s at 4-parallel (over the
// 2 s per-statement bar) with member-carrying singletons timing out outright,
// while the 16-statement key-range read passed every set isolated (warm max
// 362 ms) and concurrent (p95 323 ms). The gate is the largest A1-passing
// singleton mass halved for margin (156870/2); member-heavy mixes land above
// it and A1-passing direct-only ids stay below it. The mass probe saturates
// at HolderMassProbeCap, so the comparison needs true counts from
// HolderRoutingMassEstimator, never the capped probe mass.
const HolderKeyRangeMassGate = 78435

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
// kind (telemetry.FanOutReadKindHoldersKeyRange, or
// telemetry.FanOutReadKindHolders for the one fallback statement of
// HolderKeyRangeRead.Fallback), how long it ran, and its error, if any. The
// handler records each into the fan-out read-duration histogram.
type HolderReadStatement struct {
	Kind string
	// Range is the statement's 1-based index in its HolderKeyRanges read.
	Range    int
	Duration time.Duration
	Err      error
}

// keyRangeStatementLogMessage is the warn log line of a slow or failed
// key-range statement (noteKeyRangeStatement).
const keyRangeStatementLogMessage = "supply chain impact holder key-range statement slow or failed"

// fanOutHolderSlowStatement is the per-statement bar of the holder read
// (p95 under 2 s, #10643): a key-range statement at or over it is reported
// by noteKeyRangeStatement, which also reports every failed or fallback
// statement.
const fanOutHolderSlowStatement = 2 * time.Second

// lookupHoldsNonCVEID reports whether ids hold any id without the CVE- prefix,
// such as a GHSA alias. Only then can the foreign-member token arm of a
// key-range statement match rows: a hot CVE id has no foreign rows on QA.
func lookupHoldsNonCVEID(ids []string) bool {
	for _, id := range ids {
		if !strings.HasPrefix(strings.ToUpper(id), "CVE-") {
			return true
		}
	}
	return false
}

// HolderKeyRangeRead is the result of one key-range holder read: the holder
// scopes and every statement that ran, in order, including a failed last one.
type HolderKeyRangeRead struct {
	Scopes     []string
	Statements []HolderReadStatement
	// Fallback is true when a lookup id was too long to be tokenized, so the
	// read ran as one shipped holder statement instead of the key ranges.
	Fallback bool
}

// HolderRoutingMassEstimator is the optional seam behind the #10972 H2 A2
// key-range gate (routing-mass semantics since Lane-1b). The production
// probe reports every hot id's mass capped at HolderMassProbeCap, which
// cannot separate a 5k-row mid-band id from a 157k-row hot one; this seam
// reports the routing mass per requested lookup id so the router can
// compare it against HolderKeyRangeMassGate. The report is exact at or
// under the gate for direct-only and member<=100 ids and reports at least
// HolderKeyRangeMassGate+1 above it everywhere else (saturating high is
// the safe direction: a member mass over 100 rows ranges, so the member
// blind band can only over-route, never under-route). It reports one entry
// for every requested id (zero for no match), runs under the same 10 s
// guard as the holder read, and fails the same way, so a pass never routes
// on unmeasured ids.
type HolderRoutingMassEstimator interface {
	// EstimateHolderRoutingMasses returns the routing mass per lookup id:
	// exact at or under HolderKeyRangeMassGate for direct-only and
	// member<=100 ids, saturating at HolderKeyRangeMassGate+1 above it.
	EstimateHolderRoutingMasses(ctx context.Context, lookupIDs []string) (map[string]int64, error)
}

// HolderKeyRangeReader is the optional seam behind bounded hot-id holder reads
// (#10643, gated by HolderKeyRangeMassGate since #10972 H2 A2). A hot
// singleton id whose routing mass (HolderRoutingMassEstimator) sits above
// the gate reads as one statement per HolderKeyRanges range. Each statement carries
// the cve_id and advisory_id arms over its fact_id range and the
// advisory-group member arm as a foreign-member token arm over its share of
// the token buckets (#10643 PR 2), so no statement reads all of the id's rows
// from any arm. All of them run in one read-only REPEATABLE READ transaction,
// so they share one snapshot and their union equals FindHolderScopes for that
// id on that snapshot, a generation flip mid-read included. Each statement
// keeps the 10 s holder-read guard and the typed timeout error. A lookup id
// over 1,024 bytes has no foreign-member token, so such a read runs the
// shipped singleton statement on the same snapshot and reports
// HolderKeyRangeRead.Fallback (#10643 PR 2, ruling 10643m). A port without
// the seam keeps the shipped singleton statement.
type HolderKeyRangeReader interface {
	// FindHolderScopesByKeyRange returns the scopes, other than ownScopeID,
	// holding an active finding row for lookupIDs, read as one statement per
	// range on one snapshot. On error the
	// returned read still lists the statements that ran.
	FindHolderScopesByKeyRange(ctx context.Context, ownScopeID string, lookupIDs []string, ranges []HolderKeyRange) (HolderKeyRangeRead, error)
}
