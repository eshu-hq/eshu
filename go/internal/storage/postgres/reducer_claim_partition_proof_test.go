// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"crypto/md5" //nolint:gosec // non-cryptographic partition hash; see laneOf.
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Hermetic design proof for #11430.
// No database, no build tags: every test runs in the default `go test`.
// The election and watermark models below are shim-grade replicas of the
// ORDER BY / GREATEST rules named in comments; the structural test at the
// bottom guards the shipped SQL text directly. This file is proof, not
// production: a future implementation PR must wire a real lane predicate and
// add the live claim tests from the concurrency skill matrix.

// laneOf replicates the design's SQL lane function
// SUBSTRING(md5(conflict_domain || '|' || ckey) FROM 1 FOR 1):
// the first hex char of the MD5 of the same bytes. Postgres md5(text)
// digests database-encoded bytes; Go md5 over the same UTF-8 string
// agrees, which TestPartitionLaneMatchesPostgresMD5Vectors pins.
func laneOf(conflictDomain, ckey string) byte {
	// '|' separates the controlled domain alphabet (snake_case, never '|')
	// from the free-form key. A key containing '|' cannot break correctness:
	// lane assignment needs determinism, not injectivity — a cross-domain
	// digest collision only co-locates two unrelated keys in one lane, and
	// representative election stays per (conflict_domain, ckey).
	sum := md5.Sum([]byte(conflictDomain + "|" + ckey))
	var buf [2]byte
	hex.Encode(buf[:], sum[:1])
	return buf[0]
}

// TestPartitionLaneMatchesPostgresMD5Vectors pins laneOf against
// `SELECT substring(md5('scope' || chr(0) || 'alpha') from 1 for 1)`-style
// vectors verified once against Postgres 18 (md5 is a stable byte
// function, unlike hashtext). If the lane function ever changes, these
// vectors must be re-verified, not edited to match.
func TestPartitionLaneMatchesPostgresMD5Vectors(t *testing.T) {
	t.Parallel()

	// Verified: md5('scope|alpha') = f6dd8941ba691934aa97f3978a44b25a,
	// so substring(... from 1 for 1) = 'f'. Re-verify against a live
	// Postgres on any lane-function change.
	if got := laneOf("scope", "alpha"); got != 'f' {
		t.Fatalf("laneOf(scope, alpha) = %q, want 'f' (Postgres md5 vector)", got)
	}
	if got := laneOf("advisory_group", ""); got != laneOf("advisory_group", "") {
		t.Fatalf("laneOf not deterministic: %q", got)
	}
	// NULL conflict_key falls back to scope_id exactly as base.ckey does,
	// so the fallback key must still hash into the lane space, not panic.
	if got := laneOf("scope", "scope-id-fallback"); got < '0' || (got > '9' && got < 'a') || got > 'f' {
		t.Fatalf("laneOf(scope, fallback) = %q, want a hex char", got)
	}
}

// TestPartitionRangesTileKeySpace proves the lane-char assignment is a
// partition: lane char sets are pairwise disjoint and jointly cover all 16
// hex chars, so every canonical key lands in exactly one lane — including
// empty, multibyte, 0xff, and long keys.
func TestPartitionRangesTileKeySpace(t *testing.T) {
	t.Parallel()

	lanes := map[string][]byte{
		"lane-a": {'0', '1', '2', '3'},
		"lane-b": {'4', '5', '6', '7'},
		"lane-c": {'8', '9', 'a', 'b'},
		"lane-d": {'c', 'd', 'e', 'f'},
	}
	seen := map[byte]string{}
	for lane, chars := range lanes {
		for _, c := range chars {
			if owner, dup := seen[c]; dup {
				t.Fatalf("hex char %q in both %s and %s: lanes overlap", c, owner, lane)
			}
			seen[c] = lane
		}
	}
	if len(seen) != 16 {
		t.Fatalf("lane chars cover %d hex chars, want 16 (full tile)", len(seen))
	}

	corpus := []string{"", "a", "scope/1", "scope/\xff", "日本語キー", strings.Repeat("k", 1024)}
	for i := 0; i < 5000; i++ {
		corpus = append(corpus, fmt.Sprintf("scope-%d", i))
	}
	nonEmpty := map[string]bool{}
	for _, key := range corpus {
		lane, ok := seen[laneOf("scope", key)]
		if !ok {
			t.Fatalf("key %q hashes outside every lane", key)
		}
		nonEmpty[lane] = true
	}
	if len(nonEmpty) != len(lanes) {
		t.Fatalf("only %d/%d lanes admit corpus keys, want all (dead lane)", len(nonEmpty), len(lanes))
	}
}

// modelRow is one candidate row for the representative-election proof.
type modelRow struct {
	id      string
	key     string
	updated int
	holder  bool // claimed/running row: sorts first, like claimed_running_first = 0
	fenced  bool // live-lease-fenced: excluded from candidates, like live_fence_keys
}

// electWinners replicates the w_same representative rule
// (claimed/running holder first, then updated_at, then work_item_id) over
// the unfenced rows of one key set. The is_search_doc tie-break is
// domain-orthogonal and omitted: it cannot change cross-lane equality
// because both sides would apply it identically.
func electWinners(rows []modelRow) []string {
	byKey := map[string][]modelRow{}
	for _, r := range rows {
		if r.fenced {
			continue
		}
		byKey[r.key] = append(byKey[r.key], r)
	}
	var winners []string
	for _, group := range byKey {
		sort.Slice(group, func(i, j int) bool {
			if group[i].holder != group[j].holder {
				return group[i].holder
			}
			if group[i].updated != group[j].updated {
				return group[i].updated < group[j].updated
			}
			return group[i].id < group[j].id
		})
		winners = append(winners, group[0].id)
	}
	sort.Strings(winners)
	return winners
}

// TestPartitionedRepresentativeElectionEqualsGlobal proves keyed-equals-full
// under partitioning: per-lane election unioned over lanes equals the global
// election, because the representative rule is per-key and every key hashes
// to exactly one lane. The set carries a multi-row key (holder beats pending
// sibling), an expired-hold reclaim row, and a live-lease-fenced key.
func TestPartitionedRepresentativeElectionEqualsGlobal(t *testing.T) {
	t.Parallel()

	rows := []modelRow{
		{id: "sibling-old", key: "k1", updated: 1},
		{id: "holder", key: "k1", updated: 9, holder: true},
		{id: "reclaim", key: "k2", updated: 2, holder: true},
		{id: "solo", key: "k3", updated: 5},
		// k4 models a key with a live lease elsewhere: the holder is not in
		// the unleased base set and the pending sibling is cut by the
		// live-lease fence, so the key contributes zero candidates.
		{id: "fenced-a", key: "k4", updated: 1, fenced: true},
		{id: "fenced-b", key: "k4", updated: 2, fenced: true},
		{id: "tie-a", key: "k5", updated: 7},
		{id: "tie-b", key: "k5", updated: 7},
	}
	global := electWinners(rows)

	laned := map[byte][]modelRow{}
	for _, r := range rows {
		lane := laneOf("scope", r.key)
		laned[lane] = append(laned[lane], r)
	}
	var union []string
	for _, subset := range laned {
		union = append(union, electWinners(subset)...)
	}
	sort.Strings(union)

	if fmt.Sprintf("%v", global) != fmt.Sprintf("%v", union) {
		t.Fatalf("partitioned election %v != global election %v", union, global)
	}
	if len(global) != 4 { // k1 holder, k2 reclaim, k3 solo, k5 tie-a; k4 fully fenced
		t.Fatalf("global election = %v, want 4 winners with k4 fenced out", global)
	}
}

// TestWatermarkAdvanceIsMonotonicUnderPartitionedWriters proves lanes cannot
// regress the singleton winners watermark: every advance goes through a
// GREATEST compare-and-swap (the #11117 upsert rule), so a late commit with
// an older stamp loses the race and the final watermark is the max.
func TestWatermarkAdvanceIsMonotonicUnderPartitionedWriters(t *testing.T) {
	t.Parallel()

	var watermark atomic.Int64
	advance := func(stamp int64) {
		for {
			cur := watermark.Load()
			if stamp <= cur {
				return
			}
			if watermark.CompareAndSwap(cur, stamp) {
				return
			}
		}
	}

	const writers = 8
	const perWriter = 200
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			// Interleave fresh, duplicate, and late (older) stamps.
			for i := 0; i < perWriter; i++ {
				advance(int64((i*37+w*101)%500 + 1))
			}
			advance(int64(1000 + w)) // every lane eventually commits past the interleaving
		}(w)
	}
	wg.Wait()

	if got, want := watermark.Load(), int64(1000+writers-1); got != want {
		t.Fatalf("watermark = %d, want %d (max of all lane commits)", got, want)
	}
	// A late commit with a below-current stamp must not move the watermark.
	advance(1)
	if got := watermark.Load(); got != int64(1000+writers-1) {
		t.Fatalf("late commit regressed watermark to %d", got)
	}
}

// TestPartitionPredicateBelongsOnSnapshotSide is the structural guard the
// partitioned implementation must keep: FOR UPDATE appears only on the two
// predicate-bearing lock arms (both join lock_target by primary key to the
// snapshot ordered_candidate), and the per-key representative fence is
// intact — so a snapshot-level lane predicate partitions the lock loop
// without moving FOR UPDATE and without a new EvalPlanQual live test.
func TestPartitionPredicateBelongsOnSnapshotSide(t *testing.T) {
	t.Parallel()

	uncommented := regexp.MustCompile(`(?m)--.*$`).ReplaceAllString(claimReducerWorkBatchQuery, "")
	if got := strings.Count(uncommented, "FOR UPDATE"); got != 2 {
		t.Fatalf("FOR UPDATE appears %d times in the batch claim query, want exactly 2 "+
			"(one per locking arm; a lane predicate must not add or move one)", got)
	}
	if got := strings.Count(uncommented, "FOR UPDATE OF lock_target SKIP LOCKED"); got != 2 {
		t.Fatalf("lock-arm clause appears %d times, want exactly 2", got)
	}
	if got := strings.Count(uncommented, "AND lock_target.work_item_id = ordered_candidate.work_item_id"); got != 2 {
		t.Fatalf("PK join to the snapshot candidate side appears %d times, want exactly 2 "+
			"(both arms must lock through ordered_candidate or a lane predicate will not partition them)", got)
	}
	if got := strings.Count(uncommented, "reps.same_rn = 1"); got != 1 {
		t.Fatalf("representative fence reps.same_rn = 1 appears %d times, want exactly 1", got)
	}
}
