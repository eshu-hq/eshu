// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

var deltaUnionTestObserved = time.Date(2026, time.October, 10, 8, 0, 0, 0, time.UTC)

func deltaUnionFactRow(factID, generation, kind, key string, payload string) []any {
	return []any{
		factID,
		"scope-1",
		generation,
		kind,
		key,
		"1.0.0",
		"git",
		int64(0),
		"observed",
		"git",
		"fact-key",
		"file:///repo/path",
		"record-1",
		deltaUnionTestObserved,
		false,
		[]byte(payload),
	}
}

func deltaUnionDeltaRows() [][]any {
	return [][]any{
		deltaUnionFactRow("delta-repo", "gen-delta-1", "repository", "repository:repo-1",
			`{"repo_id":"repo-1","local_path":"/repos/alpha","delta_generation":true,"delta_relative_paths":["keep.go"],"delta_deleted_relative_paths":["w.go"]}`),
		deltaUnionFactRow("delta-keep", "gen-delta-1", "file", "file:repo-1:keep.go",
			`{"repo_id":"repo-1","relative_path":"keep.go"}`),
	}
}

func deltaUnionBaseRows() [][]any {
	return [][]any{
		deltaUnionFactRow("base-repo", "gen-base-1", "repository", "repository:repo-1",
			`{"repo_id":"repo-1","local_path":"/repos/alpha"}`),
		deltaUnionFactRow("base-keep", "gen-base-1", "file", "file:repo-1:keep.go",
			`{"repo_id":"repo-1","relative_path":"keep.go"}`),
		deltaUnionFactRow("base-w", "gen-base-1", "file", "file:repo-1:w.go",
			`{"repo_id":"repo-1","relative_path":"w.go"}`),
		deltaUnionFactRow("base-other", "gen-base-1", "file", "file:repo-1:other.go",
			`{"repo_id":"repo-1","relative_path":"other.go"}`),
	}
}

func deltaUnionFactIDs(t *testing.T, factKinds map[string]string) []string {
	t.Helper()
	ids := make([]string, 0, len(factKinds))
	for id := range factKinds {
		ids = append(ids, id)
	}
	return ids
}

func TestFactStoreListFactsFullGenerationSkipsUnion(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{false, ""}}},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-full-1")
	if err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 2; got != want {
		t.Fatalf("ListFacts() len = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d (facts plus target row, no base lookup)", got, want)
	}
}

func TestFactStoreListFactsDeltaOverlaysBase(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-base"}}},
			{rows: [][]any{{"gen-base-1", "active"}}},
			{rows: deltaUnionBaseRows()},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}
	byID := make(map[string]string, len(loaded))
	for _, env := range loaded {
		byID[env.FactID] = env.GenerationID
	}
	// Delta rows win: the delta repo fact replaces the base repo fact and
	// the changed file replaces its base row; the deleted file's base row
	// is gone while the untouched base row survives.
	for _, id := range []string{"delta-repo", "delta-keep", "base-other"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("union is missing fact %q: %v", id, deltaUnionFactIDs(t, byID))
		}
	}
	for _, absent := range []string{"base-repo", "base-keep", "base-w"} {
		if _, ok := byID[absent]; ok {
			t.Fatalf("union contains dropped base fact %q", absent)
		}
	}
	// Base rows keep their own generation identity.
	if got := byID["base-other"]; got != "gen-base-1" {
		t.Fatalf("base fact generation = %q, want gen-base-1", got)
	}
	if got, want := len(db.queries), 4; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[1].query, "delta_baseline_commit_sha") {
		t.Fatalf("second query = %q, want generation target read", db.queries[1].query)
	}
	if !strings.Contains(db.queries[2].query, "activated_at IS NOT NULL") {
		t.Fatalf("third query = %q, want projected-base lookup", db.queries[2].query)
	}
}

func TestFactStoreListFactsDeltaBaseMissingRefuses(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-pruned"}}},
			{rows: nil},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if loaded != nil {
		t.Fatalf("ListFacts() = %d facts, want nil on refusal", len(loaded))
	}
	var missing *projector.DeltaBaseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("ListFacts() error = %v, want DeltaBaseMissingError", err)
	}
	if missing.ScopeID != "scope-1" || missing.GenerationID != "gen-delta-1" || missing.BaselineCommitSHA != "commit-pruned" {
		t.Fatalf("refusal = %+v, want scope/generation/baseline", missing)
	}
}

func TestFactStoreListFactsLegacyDeltaLoadsSingleGeneration(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, ""}}},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 2; got != want {
		t.Fatalf("ListFacts() len = %d, want %d (no union without a baseline)", got, want)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
}

func TestFactStoreCountFactsDeltaReturnsUnionBound(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{true, "commit-base"}}},
			{rows: [][]any{{"gen-base-1", "active"}}},
			{rows: [][]any{{90}}},
			{rows: [][]any{{5}}},
		},
	}
	store := NewFactStore(db)

	count, err := store.CountFacts(context.Background(), "scope-1", "gen-delta-1")
	if err != nil {
		t.Fatalf("CountFacts() error = %v, want nil", err)
	}
	if got, want := count, 95; got != want {
		t.Fatalf("CountFacts() = %d, want %d (base plus delta)", got, want)
	}
	if got, want := len(db.queries), 4; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
}

func TestFactStoreCountFactsBaseMissingRefuses(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{true, "commit-pruned"}}},
			{rows: nil},
		},
	}
	store := NewFactStore(db)

	_, err := store.CountFacts(context.Background(), "scope-1", "gen-delta-1")
	var missing *projector.DeltaBaseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("CountFacts() error = %v, want DeltaBaseMissingError", err)
	}
}

// The base-lookup predicates must keep matching the partial union index
// (migration 178): a predicate edit that drops the planner off the index is
// a silent per-projection rescan of the scope's generation history.
func TestDeltaUnionBaseQueryKeepsPartialIndexPredicates(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"scope_id = $1",
		"is_delta = false",
		"activated_at IS NOT NULL",
		"status IN ('active', 'completed', 'superseded')",
		"source_commit_sha = $2",
		"ORDER BY (status IN ('active', 'completed')) DESC, ingested_at DESC, generation_id DESC",
	} {
		if !strings.Contains(deltaUnionBaseQuery, fragment) {
			t.Fatalf("deltaUnionBaseQuery is missing %q", fragment)
		}
	}
}

// An active base skips the retention probe: active generations are never
// prune-eligible, so the union costs no extra query for the common case.
func TestFactStoreListFactsActiveBaseSkipsPruneProbe(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-base"}}},
			{rows: [][]any{{"gen-base-1", "active"}}},
			{rows: deltaUnionBaseRows()},
		},
	}
	store := NewFactStore(db)

	if _, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1"); err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 4; got != want {
		t.Fatalf("query count = %d, want %d (no prune probe for an active base)", got, want)
	}
}

// A superseded base with no retention event overlays normally after one
// indexed probe.
func TestFactStoreListFactsSupersededBaseWithoutPruneEventOverlays(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-base"}}},
			{rows: [][]any{{"gen-base-1", "superseded"}}},
			{rows: nil},
			{rows: deltaUnionBaseRows()},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}
	if got, want := len(loaded), 3; got != want {
		t.Fatalf("ListFacts() len = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 5; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[3].query, "generation_retention_events") {
		t.Fatalf("fourth query = %q, want the retention probe", db.queries[3].query)
	}
}

// A superseded base with a retention prune event refuses: retention may
// have taken part of its facts, so overlaying would restore only a truncated
// tree.
func TestFactStoreListFactsPrunedSupersededBaseRefuses(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-base"}}},
			{rows: [][]any{{"gen-base-1", "superseded"}}},
			{rows: [][]any{{1}}},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if loaded != nil {
		t.Fatalf("ListFacts() = %d facts, want nil on refusal", len(loaded))
	}
	var missing *projector.DeltaBaseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("ListFacts() error = %v, want DeltaBaseMissingError", err)
	}
	if got, want := len(db.queries), 4; got != want {
		t.Fatalf("query count = %d, want %d (no base facts read after refusal)", got, want)
	}
}

// A base row with no facts refuses: a stored full generation always carries
// at least its repository fact, so an empty read is a drained base.
func TestFactStoreListFactsEmptyBaseRefuses(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-base"}}},
			{rows: [][]any{{"gen-base-1", "active"}}},
			{rows: nil},
		},
	}
	store := NewFactStore(db)

	loaded, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1")
	if loaded != nil {
		t.Fatalf("ListFacts() = %d facts, want nil on refusal", len(loaded))
	}
	var missing *projector.DeltaBaseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("ListFacts() error = %v, want DeltaBaseMissingError", err)
	}
}
