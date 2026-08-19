// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestSelectPartitionBatchFiltersBeforeDeduping pins the order of
// FilterAuthoritativeIntents and LatestIntentsByRepoAndPartition inside
// SelectPartitionBatch, and pins that dedup consumes the filter's output.
//
// The order is load-bearing and the compiler does not protect it. Both
// functions take []SharedProjectionIntentRow first and return
// []SharedProjectionIntentRow first, so swapping them type-checks; the reordered
// form compiles and no other test in this repository fails on it. It also has an
// attractive rationale -- dedup first to shrink the set before the per-key
// acceptance lookup -- which makes it something a future change may reach for on
// purpose rather than by accident.
//
// Reordering is not merely inelegant, it drops work. GenerationID is NOT part of
// repoPartitionKey (scopeID, acceptanceUnitID, sourceRunID, repositoryID,
// partitionKey), and SourceRunID and GenerationID are independent fields on the
// row. So two rows for one repository that share a dedup key but differ in
// GenerationID behave differently under each order:
//
//   - filter first (correct): the stale-generation row is dropped as stale, and
//     dedup then sees only the accepted row.
//   - dedup first: both share the dedup key and collapse to ONE survivor,
//     chosen refresh-first, then LATEST CreatedAt, then largest IntentID on a
//     tie -- which can be the STALE row. The sort is ascending and the map
//     keeps the last row written per key, so the survivor is the newest, not
//     the oldest. FilterAuthoritativeIntents then drops that survivor as
//     stale, and the repository loses its refresh for that cycle entirely.
//
// The failure is a LOST refresh, not a double retract: the whole-scope retract
// never runs and stale EXPLAINS edges persist with no error and no dead letter.
// storage/cypher's collectWholeScopeRefreshRepoIDs godoc names this as one of
// the three ways its disjointness invariant breaks (#6171 review).
//
// This is a source-level assertion because the property is about call order in
// one function rather than about a value any caller can observe, matching the
// source-grep tests in cmd/reducer/neo4j_wiring_test.go.
func TestSelectPartitionBatchFiltersBeforeDeduping(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("shared_projection_worker.go")
	if err != nil {
		t.Fatalf("read shared_projection_worker.go: %v", err)
	}

	body := selectPartitionBatchBody(t, string(src))

	filterIdx := strings.Index(body, "FilterAuthoritativeIntents(")
	dedupIdx := strings.Index(body, "LatestIntentsByRepoAndPartition(")
	if filterIdx < 0 {
		t.Fatal("SelectPartitionBatch no longer calls FilterAuthoritativeIntents; the acceptance fence is gone")
	}
	if dedupIdx < 0 {
		t.Fatal("SelectPartitionBatch no longer calls LatestIntentsByRepoAndPartition; the per-(repo,partition) dedup is gone")
	}
	if filterIdx > dedupIdx {
		t.Fatalf("SelectPartitionBatch dedupes before filtering: FilterAuthoritativeIntents at %d, LatestIntentsByRepoAndPartition at %d.\n"+
			"Dedup must run on the filter's output. GenerationID is not part of the dedup key, so deduping first can collapse an accepted row and a stale-generation row into one survivor, keep the stale one, and then drop it as stale -- losing that repository's refresh for the cycle.",
			filterIdx, dedupIdx)
	}

	// Order alone is not enough: dedup must consume the filter's OUTPUT, not the
	// raw rows. Both calls could appear in the right order while dedup still ran
	// on the unfiltered slice.
	filterOut := firstReturnIdent(t, body, filterIdx, "FilterAuthoritativeIntents(")
	dedupArg := callArgument(t, body, dedupIdx, "LatestIntentsByRepoAndPartition(")
	if dedupArg != filterOut {
		t.Fatalf("LatestIntentsByRepoAndPartition(%s) does not consume FilterAuthoritativeIntents' output (%s).\n"+
			"Dedup must run on the filtered rows; running it on the raw partition rows reintroduces the cross-generation collapse this order exists to prevent.",
			dedupArg, filterOut)
	}
}

// selectPartitionBatchBody returns the source text of SelectPartitionBatch, from
// its declaration to the next top-level func.
func selectPartitionBatchBody(t *testing.T, src string) string {
	t.Helper()

	start := strings.Index(src, "func SelectPartitionBatch(")
	if start < 0 {
		t.Fatal("SelectPartitionBatch not found in shared_projection_worker.go")
	}
	rest := src[start+1:]
	if end := strings.Index(rest, "\nfunc "); end >= 0 {
		return rest[:end]
	}
	return rest
}

var identAssignRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*,\s*[A-Za-z_][A-Za-z0-9_]*\s*:?=\s*$`)

// firstReturnIdent returns the identifier bound to the first result of the call
// starting at idx, e.g. "active" for `active, staleIDs := Filter...(`.
func firstReturnIdent(t *testing.T, body string, idx int, call string) string {
	t.Helper()

	lineStart := strings.LastIndex(body[:idx], "\n") + 1
	prefix := body[lineStart:idx]
	m := identAssignRe.FindStringSubmatch(prefix)
	if m == nil {
		t.Fatalf("could not read the identifier assigned from %s; the call shape changed: %q", call, strings.TrimSpace(prefix))
	}
	return m[1]
}

// callArgument returns the first argument text of the call starting at idx.
func callArgument(t *testing.T, body string, idx int, call string) string {
	t.Helper()

	open := idx + len(call)
	closeIdx := strings.IndexAny(body[open:], ",)")
	if closeIdx < 0 {
		t.Fatalf("could not read the argument passed to %s; the call shape changed", call)
	}
	return strings.TrimSpace(body[open : open+closeIdx])
}
