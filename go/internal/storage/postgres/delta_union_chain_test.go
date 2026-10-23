// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestFactStoreListFactsChainedDeltaResolvesBaseAtUnionBase pins the #8780
// base rule: a cumulative delta's base resolves at its recorded union-base
// commit, not at its #7319 fence commit, and the union decision log names
// both commits and marks the delta chained.
func TestFactStoreListFactsChainedDeltaResolvesBaseAtUnionBase(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-active-delta", "commit-full"}}},
			{rows: [][]any{{"gen-base-1", "superseded"}}},
			{rows: nil},
			{rows: deltaUnionBaseRows()},
		},
	}
	var logs bytes.Buffer
	store := NewFactStore(db).WithLogger(slog.New(slog.NewJSONHandler(&logs, nil)))

	if _, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1"); err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}
	if !strings.Contains(db.queries[1].query, "delta_union_base_commit_sha") {
		t.Fatalf("target read = %q, want it to read delta_union_base_commit_sha", db.queries[1].query)
	}
	if got := db.queries[2].args; len(got) != 2 || got[1] != "commit-full" {
		t.Fatalf("base lookup args = %#v, want the union-base commit commit-full", got)
	}
	for _, want := range []string{
		`"delta_baseline_commit_sha":"commit-active-delta"`,
		`"delta_union_base_commit_sha":"commit-full"`,
		`"chained":true`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("union decision log missing %s:\n%s", want, logs.String())
		}
	}
}

// TestFactStoreListFactsLegacyDeltaResolvesBaseAtBaseline keeps the legacy
// shape: a delta stored before #8780 has no union-base column, so its base
// resolves at COALESCE(union base, baseline) = its baseline, and it is not
// chained.
func TestFactStoreListFactsLegacyDeltaResolvesBaseAtBaseline(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-base", ""}}},
			{rows: [][]any{{"gen-base-1", "active"}}},
			{rows: deltaUnionBaseRows()},
		},
	}
	var logs bytes.Buffer
	store := NewFactStore(db).WithLogger(slog.New(slog.NewJSONHandler(&logs, nil)))

	if _, err := store.ListFacts(context.Background(), "scope-1", "gen-delta-1"); err != nil {
		t.Fatalf("ListFacts() error = %v, want nil", err)
	}
	if got := db.queries[2].args; len(got) != 2 || got[1] != "commit-base" {
		t.Fatalf("base lookup args = %#v, want the baseline commit-base", got)
	}
	if !strings.Contains(logs.String(), `"chained":false`) {
		t.Fatalf("union decision log does not mark the legacy delta unchained:\n%s", logs.String())
	}
}

// TestFactStoreListFactsChainedDeltaBaseMissingNamesBothCommits proves the
// fail-closed refusal of a chained delta whose union base is gone carries the
// fence commit and the union-base commit the lookup actually used.
func TestFactStoreListFactsChainedDeltaBaseMissingNamesBothCommits(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: deltaUnionDeltaRows()},
			{rows: [][]any{{true, "commit-active-delta", "commit-drained-full"}}},
			{rows: nil},
		},
	}
	_, err := NewFactStore(db).ListFacts(context.Background(), "scope-1", "gen-delta-1")
	var missing *projector.DeltaBaseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("ListFacts() error = %v, want DeltaBaseMissingError", err)
	}
	if missing.BaselineCommitSHA != "commit-active-delta" || missing.UnionBaseCommitSHA != "commit-drained-full" {
		t.Fatalf("refusal = %+v, want baseline commit-active-delta and union base commit-drained-full", missing)
	}
}
