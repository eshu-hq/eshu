// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// changedSinceCountsByCategory runs the shipped single statement and returns the
// counts and sample keys per category and classification.
func changedSinceCountsByCategory(
	ctx context.Context,
	t *testing.T,
	tx *sql.Tx,
	scope string,
) map[statuspkg.ChangedSinceCategory]statuspkg.ChangedSinceCategoryDelta {
	t.Helper()
	categories, err := StatusStore{queryer: &countingTxQueryer{tx: tx}}.
		changedSinceCategories(ctx, scope, "prior", "current", 25)
	if err != nil {
		t.Fatalf("changedSinceCategories: %v", err)
	}
	byCategory := map[statuspkg.ChangedSinceCategory]statuspkg.ChangedSinceCategoryDelta{}
	for _, category := range categories {
		byCategory[category.Category] = category
	}
	return byCategory
}

func sampleKeys(delta statuspkg.ChangedSinceCategoryDelta, classification statuspkg.ChangedSinceClassification) []string {
	var keys []string
	for _, sample := range delta.Samples[classification] {
		keys = append(keys, sample.StableFactKey)
	}
	return keys
}

// TestChangedSinceIgnoresIndexedAtChurnOnContentEntitiesLive is the #7127 A1
// proof: content_entity payloads differ between generations only in the
// per-run indexed_at timestamp the collector stamps, which is not repository
// change, so those keys are unchanged. A real content change still reports
// updated even when indexed_at also moved, and only content_entity is
// normalized.
func TestChangedSinceIgnoresIndexedAtChurnOnContentEntitiesLive(t *testing.T) {
	ctx, tx := openChangedSinceFixtureTx(t)
	rows := []changedSinceFixtureRow{}
	add := func(more ...changedSinceFixtureRow) { rows = append(rows, more...) }
	add(pair("content_entity", "entity/churn-only",
		`{"entity_name":"f","indexed_at":"2026-09-23T00:00:00Z"}`,
		`{"entity_name":"f","indexed_at":"2026-09-25T00:00:00Z"}`)...)
	add(pair("content_entity", "entity/real-change",
		`{"entity_name":"f","indexed_at":"2026-09-23T00:00:00Z"}`,
		`{"entity_name":"g","indexed_at":"2026-09-25T00:00:00Z"}`)...)
	// A key with duplicate rows whose multiset differs only in indexed_at goes
	// through the multiset comparison and must normalize there too.
	add(
		changedSinceFixtureRow{"n1", "prior", "content_entity", "entity/dup-churn", `{"n":"a","indexed_at":"t1"}`, false},
		changedSinceFixtureRow{"n1", "prior", "content_entity", "entity/dup-churn", `{"n":"b","indexed_at":"t1"}`, false},
		changedSinceFixtureRow{"n1", "current", "content_entity", "entity/dup-churn", `{"n":"a","indexed_at":"t2"}`, false},
		changedSinceFixtureRow{"n1", "current", "content_entity", "entity/dup-churn", `{"n":"b","indexed_at":"t2"}`, false},
	)
	// Only content_entity is normalized: the same field on another kind is a
	// real payload difference.
	add(pair("file", "file/indexed-at", `{"indexed_at":"t1"}`, `{"indexed_at":"t2"}`)...)
	for i := range rows {
		rows[i].scope = "n1"
	}
	insertChangedSinceFixtureRows(ctx, t, tx, rows)

	got := changedSinceCountsByCategory(ctx, t, tx, "n1")
	entities := got[statuspkg.ChangedSinceCategoryContentEntities]
	if want := (statuspkg.ChangedSinceCounts{Updated: 1, Unchanged: 2}); entities.Counts != want {
		t.Fatalf("content_entities counts = %+v, want %+v (indexed_at churn must not count as change)", entities.Counts, want)
	}
	if keys := sampleKeys(entities, statuspkg.ChangedSinceUpdated); !slices.Equal(keys, []string{"entity/real-change"}) {
		t.Fatalf("updated content entities = %v, want only the real change", keys)
	}
	files := got[statuspkg.ChangedSinceCategoryFiles]
	if want := (statuspkg.ChangedSinceCounts{Updated: 1}); files.Counts != want {
		t.Fatalf("files counts = %+v, want %+v (indexed_at is only normalized on content_entity)", files.Counts, want)
	}
}

// TestChangedSinceExcludesReducerDerivedFactsLive is the #7127 A2 proof:
// reducer-derived rows are written into a generation after it activates, so
// their presence tracks reducer scheduling, not repository change, and the
// diff must not report them as added, updated, or retired. A kind that merely
// starts with "reducer" (no underscore) is not reducer-derived.
func TestChangedSinceExcludesReducerDerivedFactsLive(t *testing.T) {
	ctx, tx := openChangedSinceFixtureTx(t)
	rows := []changedSinceFixtureRow{
		{"n1", "current", "reducer_eshu_search_document", "reducer/doc1", `{"d":1}`, false},
		{"n1", "current", "reducer_workload_identity", "reducer/id1", `{"d":1}`, false},
		{"n1", "prior", "reducer_platform_materialization", "reducer/plat", `{"d":1}`, false},
		{"n1", "current", "reducer_platform_materialization", "reducer/plat", `{"d":2}`, false},
		{"n1", "prior", "reducer_code_drifted_finding", "reducer/drift", `{"d":1}`, false},
		{"n1", "current", "reducer_code_drifted_finding", "reducer/drift", `{"d":1}`, true},
		{"n1", "current", "reducerX_not_derived", "reducerx/added", `{"d":1}`, false},
		{"n1", "prior", "aws_resource", "fact/stays", `{"v":1}`, false},
		{"n1", "current", "aws_resource", "fact/stays", `{"v":1}`, false},
	}
	insertChangedSinceFixtureRows(ctx, t, tx, rows)

	facts := changedSinceCountsByCategory(ctx, t, tx, "n1")[statuspkg.ChangedSinceCategoryFacts]
	if want := (statuspkg.ChangedSinceCounts{Added: 1, Unchanged: 1}); facts.Counts != want {
		t.Fatalf("facts counts = %+v, want %+v (reducer_* rows must not classify)", facts.Counts, want)
	}
	if keys := sampleKeys(facts, statuspkg.ChangedSinceAdded); !slices.Equal(keys, []string{"reducerx/added"}) {
		t.Fatalf("added facts = %v, want only the non-reducer-derived kind", keys)
	}
}

// TestChangedSinceRepositorySourceRunIDReportsUpdatedLive documents the ruled
// behavior: the repository fact carries source_run_id, a per-run field that
// reducers consume, so it is not normalized and the repository key reports
// updated on every new run.
func TestChangedSinceRepositorySourceRunIDReportsUpdatedLive(t *testing.T) {
	ctx, tx := openChangedSinceFixtureTx(t)
	rows := []changedSinceFixtureRow{
		{"n1", "prior", "repository", "repository:r", `{"repo_id":"r","source_run_id":"run-1"}`, false},
		{"n1", "current", "repository", "repository:r", `{"repo_id":"r","source_run_id":"run-2"}`, false},
	}
	insertChangedSinceFixtureRows(ctx, t, tx, rows)

	facts := changedSinceCountsByCategory(ctx, t, tx, "n1")[statuspkg.ChangedSinceCategoryFacts]
	if want := (statuspkg.ChangedSinceCounts{Updated: 1}); facts.Counts != want {
		t.Fatalf("facts counts = %+v, want %+v", facts.Counts, want)
	}
	if keys := sampleKeys(facts, statuspkg.ChangedSinceUpdated); !slices.Equal(keys, []string{"repository:r"}) {
		t.Fatalf("updated facts = %v, want the repository key", keys)
	}
}
