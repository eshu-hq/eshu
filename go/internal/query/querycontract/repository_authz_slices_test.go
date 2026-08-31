// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "testing"

// A family package constructs this filter from the exported fields, which is
// what the type's own doc comment tells it to do. Before these methods
// consulted the id slices, such a filter reported Empty and denied every id it
// actually granted, because the derived Allowed cache was nil. That fails
// closed rather than open, so it leaks nothing -- it silently drops a scoped
// caller's valid reads instead, which is just as wrong and much harder to see.
func TestFilterBuiltFromSlicesAloneHonoursItsGrants(t *testing.T) {
	filter := RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-a"}}

	if filter.Empty() {
		t.Fatal("filter with a repository grant reports Empty; a scoped read would return zero rows")
	}
	if !filter.AllowsRepositoryID("repo-a") {
		t.Fatal("filter denies repo-a, which it explicitly grants")
	}
	if filter.AllowsRepositoryID("repo-b") {
		t.Fatal("filter allows repo-b, which it does not grant")
	}
}

func TestScopeOnlyFilterBuiltFromSlicesAloneHonoursItsGrants(t *testing.T) {
	filter := RepositoryAccessFilter{AllowedScopeIDs: []string{"scope-a"}}

	if filter.Empty() {
		t.Fatal("filter with a scope grant reports Empty")
	}
	if !filter.AllowsRepositoryID("scope-a") {
		t.Fatal("filter denies scope-a, which it grants through the scope list")
	}
}

// The zero value must still deny. This is the direction that would be a
// tenant-boundary failure rather than a dropped read.
func TestZeroFilterStillFailsClosed(t *testing.T) {
	var filter RepositoryAccessFilter

	if !filter.Scoped() {
		t.Fatal("zero filter reports unscoped; it would bypass every grant check")
	}
	if !filter.Empty() {
		t.Fatal("zero filter does not report Empty")
	}
	if filter.AllowsRepositoryID("repo-a") {
		t.Fatal("zero filter allows repo-a")
	}
	if filter.AllowsRepositoryID("") {
		t.Fatal("zero filter allows the empty id")
	}
}

// A correctly-built filter, with the cache populated, keeps behaving exactly as
// it did before the slices became authoritative.
func TestCachePopulatedFilterUnchanged(t *testing.T) {
	filter := RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
		Allowed:              map[string]struct{}{"repo-a": {}, "scope-a": {}},
	}

	if filter.Empty() {
		t.Fatal("populated filter reports Empty")
	}
	for _, id := range []string{"repo-a", "scope-a"} {
		if !filter.AllowsRepositoryID(id) {
			t.Fatalf("populated filter denies granted id %q", id)
		}
	}
	if filter.AllowsRepositoryID("repo-z") {
		t.Fatal("populated filter allows an ungranted id")
	}
}
