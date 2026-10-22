// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"context"
	"reflect"
	"runtime"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// tiePair returns two tools with identical text so only catalog data orders them.
func tiePair() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		def("alpha", "Locates the zebra."),
		def("beta", "Locates the zebra."),
	}
}

// newRankerWithUnreachableMap builds a ranker over a Canonical-beta entries map
// that only the ranker's index can reach once this returns, and reports the
// map's address.
func newRankerWithUnreachableMap(tools []toolcontract.ToolDefinition) (*Lexical, uintptr) {
	entries := map[string]catalog.Entry{"beta": {Name: "beta", Canonical: true}}
	return New(tools, entries), reflect.ValueOf(entries).Pointer()
}

// sameOrderAsFresh asserts r ranks (tools, entries) exactly as a brand-new
// ranker does, including scores.
func sameOrderAsFresh(t *testing.T, r *Lexical, tools []toolcontract.ToolDefinition, entries map[string]catalog.Entry) {
	t.Helper()
	q := router.Query{Intent: "zebra", Limit: 5}
	got, err := r.Rank(context.Background(), q, tools, entries, allowAll)
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	want, err := New(tools, entries).Rank(context.Background(), q, tools, entries, allowAll)
	if err != nil {
		t.Fatalf("fresh Rank: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cached ranker = %v, fresh ranker = %v; the cache served stale data", names(got), names(want))
	}
}

// TestRankDifferentEntriesMapRebuilds: a different entries map of the same
// length must never be answered from the index built for another map.
func TestRankDifferentEntriesMapRebuilds(t *testing.T) {
	tools := tiePair()
	r := New(tools, map[string]catalog.Entry{"beta": {Name: "beta", Canonical: true}})
	other := map[string]catalog.Entry{"alpha": {Name: "alpha", Canonical: true}}
	sameOrderAsFresh(t, r, tools, other)
}

// TestRankDroppedEntriesMapCannotBeRecycled drops the original entries map,
// forces collections, and hunts for a new same-length map that lands on the old
// map's address. If the index kept only that address, the new map would be
// answered from stale Canonical data. The index holds the old map, so its
// address is never recycled and every new map must rank as a fresh ranker
// would. The test asserts that on every attempt, and logs when no collision
// occurred (the hunt is allocator-dependent; the fix makes it moot).
func TestRankDroppedEntriesMapCannotBeRecycled(t *testing.T) {
	tools := tiePair()
	r, oldAddr := newRankerWithUnreachableMap(tools)
	collided := false
	for i := 0; i < 5000 && !collided; i++ {
		if i%20 == 0 {
			runtime.GC()
		}
		next := map[string]catalog.Entry{"alpha": {Name: "alpha", Canonical: true}}
		collided = reflect.ValueOf(next).Pointer() == oldAddr
		sameOrderAsFresh(t, r, tools, next)
	}
	t.Logf("address collision with the dropped map observed: %v", collided)
}

// TestRankChangedDescriptionInNewSliceRebuilds: a new tools slice with the same
// names but a changed description must not be ranked from the old text.
func TestRankChangedDescriptionInNewSliceRebuilds(t *testing.T) {
	old := []toolcontract.ToolDefinition{def("alpha", "Locates the zebra."), def("beta", "Locates the horse.")}
	r := New(old, nil)
	changed := []toolcontract.ToolDefinition{def("alpha", "Locates the horse."), def("beta", "Locates the zebra.")}
	sameOrderAsFresh(t, r, changed, nil)
}

// TestRankSameInputsReuseIndex pins the other side: identical inputs keep
// using the one index.
func TestRankSameInputsReuseIndex(t *testing.T) {
	tools := tiePair()
	entries := map[string]catalog.Entry{"beta": {Name: "beta", Canonical: true}}
	r := New(tools, entries)
	for i := 0; i < 5; i++ {
		sameOrderAsFresh(t, r, tools, entries)
	}
	if got := r.builds.Load(); got != 1 {
		t.Fatalf("builds = %d, want 1", got)
	}
}

// TestRankIndexRetainsInputs: the index must hold the entries map and tools
// slice it was built from. That keeps the map alive, so its address cannot be
// recycled for a different map while the index lives, and it is what Rank
// compares against.
func TestRankIndexRetainsInputs(t *testing.T) {
	tools := tiePair()
	r, addr := newRankerWithUnreachableMap(tools)
	runtime.GC()
	if got := reflect.ValueOf(r.idx.entries).Pointer(); got != addr {
		t.Fatalf("index holds map at %#x, want the original at %#x", got, addr)
	}
	if len(r.idx.tools) != len(tools) || &r.idx.tools[0] != &tools[0] {
		t.Fatalf("index does not hold the original tools slice")
	}
}
