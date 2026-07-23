// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package boundedset

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

type testItem struct {
	key    string
	factID string
}

func testLess(a, b testItem) bool {
	if a.key != b.key {
		return a.key < b.key
	}
	return a.factID < b.factID
}

func testDedupeKey(a, b testItem) bool {
	return a.key == b.key
}

func TestDedupeSortCapDedupesSortsAndCapsBeforeCounting(t *testing.T) {
	t.Parallel()

	items := []testItem{
		{key: "c", factID: "fact-c"},
		{key: "a", factID: "fact-a-2"},
		{key: "a", factID: "fact-a-1"},
		{key: "b", factID: "fact-b"},
	}

	got, count := DedupeSortCap(items, testLess, testDedupeKey, 2)
	if count != 3 {
		t.Fatalf("count = %d, want 3 distinct keys", count)
	}
	want := []testItem{
		{key: "a", factID: "fact-a-1"},
		{key: "b", factID: "fact-b"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %#v, want %#v", got, want)
	}
}

func TestDedupeSortCapZeroOrNegativeMaxRowsDisablesCap(t *testing.T) {
	t.Parallel()

	items := []testItem{{key: "b", factID: "f2"}, {key: "a", factID: "f1"}}
	got, count := DedupeSortCap(items, testLess, testDedupeKey, 0)
	if count != 2 || len(got) != 2 {
		t.Fatalf("count/len = %d/%d, want 2/2 when maxRows disables capping", count, len(got))
	}
}

// TestDedupeSortCapIsOrderInvariantAcrossShuffles proves the generic engine's
// output depends only on the item set and the caller's less/dedupeKey, never
// on the input slice's original order, PROVIDED less fully orders items
// (including tiebreaking distinct-factID duplicates deterministically). This
// is the property both the reducer (write path) and query (read path)
// callers rely on for a stable, replay-order-independent bounded preview.
func TestDedupeSortCapIsOrderInvariantAcrossShuffles(t *testing.T) {
	t.Parallel()

	base := make([]testItem, 0, 12)
	for i := 0; i < 10; i++ {
		base = append(base, testItem{key: fmt.Sprintf("key-%02d", i), factID: fmt.Sprintf("fact-%02d", i)})
	}
	// Two duplicates (same key) distinguished only by fact_id: the tiebreak
	// in testLess must deterministically decide the survivor regardless of
	// shuffle order.
	base = append(base,
		testItem{key: "key-00", factID: "dup-a"},
		testItem{key: "key-00", factID: "dup-b"},
	)

	var reference []testItem
	for trial := 0; trial < 30; trial++ {
		shuffled := append([]testItem(nil), base...)
		rng := rand.New(rand.NewSource(int64(trial))) //nolint:gosec // deterministic shuffle, not security-sensitive
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

		got, count := DedupeSortCap(shuffled, testLess, testDedupeKey, 100)
		if count != 10 {
			t.Fatalf("trial %d: count = %d, want 10 distinct keys", trial, count)
		}
		if trial == 0 {
			reference = got
			continue
		}
		if !reflect.DeepEqual(got, reference) {
			t.Fatalf("trial %d: output changed across shuffled input order\nreference: %#v\ngot:       %#v", trial, reference, got)
		}
	}
	if reference[0].factID != "dup-a" {
		t.Fatalf("surviving duplicate fact_id = %q, want lexicographically smallest %q", reference[0].factID, "dup-a")
	}
}
