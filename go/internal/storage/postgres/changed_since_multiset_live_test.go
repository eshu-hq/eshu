// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"slices"
	"testing"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// TestChangedSinceDuplicatePayloadSetsLive proves counts and samples classify
// every payload in a stable-key group on a real PostgreSQL backend.
func TestChangedSinceDuplicatePayloadSetsLive(t *testing.T) {
	ctx, tx := openChangedSinceFixtureTx(t)
	insertChangedSinceFixtureRows(ctx, t, tx, []changedSinceFixtureRow{
		{"probe", "prior", "test", "dup_changed", `{"v":"G"}`, false},
		{"probe", "prior", "test", "dup_changed", `{"v":"A"}`, false},
		{"probe", "current", "test", "dup_changed", `{"v":"G"}`, false},
		{"probe", "current", "test", "dup_changed", `{"v":"B"}`, false},
		{"probe", "prior", "test", "reordered", `{"v":"G"}`, false},
		{"probe", "prior", "test", "reordered", `{"v":"A"}`, false},
		{"probe", "current", "test", "reordered", `{"v":"A"}`, false},
		{"probe", "current", "test", "reordered", `{"v":"G"}`, false},
		{"probe", "prior", "test", "multiplicity", `{"v":"A"}`, false},
		{"probe", "prior", "test", "multiplicity", `{"v":"A"}`, false},
		{"probe", "current", "test", "multiplicity", `{"v":"A"}`, false},
		{"probe", "prior", "test", "singleton", `{"v":"A"}`, false},
		{"probe", "current", "test", "singleton", `{"v":"B"}`, false},
		{"probe", "prior", "test", "retired", `{"v":"A"}`, false},
		{"probe", "current", "test", "retired", `{"v":"A"}`, true},
	})

	categories, err := StatusStore{queryer: &countingTxQueryer{tx: tx}}.
		changedSinceCategories(ctx, "probe", "prior", "current", 10)
	if err != nil {
		t.Fatalf("changed-since diff: %v", err)
	}
	var facts statuspkg.ChangedSinceCategoryDelta
	for _, category := range categories {
		if category.Category == statuspkg.ChangedSinceCategoryFacts {
			facts = category
			continue
		}
		if category.Counts != (statuspkg.ChangedSinceCounts{}) {
			t.Errorf("unexpected counts in category %s: %+v", category.Category, category.Counts)
		}
	}
	if want := (statuspkg.ChangedSinceCounts{Updated: 3, Unchanged: 1, Retired: 1}); facts.Counts != want {
		t.Errorf("facts counts = %+v, want %+v", facts.Counts, want)
	}

	var gotKeys []string
	for _, sample := range facts.Samples[statuspkg.ChangedSinceUpdated] {
		if sample.FactKind != "test" {
			t.Errorf("sample %q has fact_kind %q, want test", sample.StableFactKey, sample.FactKind)
		}
		gotKeys = append(gotKeys, sample.StableFactKey)
	}
	if want := []string{"dup_changed", "multiplicity", "singleton"}; !slices.Equal(gotKeys, want) {
		t.Errorf("updated samples = %v, want %v", gotKeys, want)
	}
}
