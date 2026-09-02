// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"reflect"
	"testing"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// grantBindPositions names where the grant predicate reads its arguments from.
// The SQL spells these as placeholders, so the positions are part of the
// contract: a scoped flag that drifts to another index leaves the predicate
// reading whatever now sits at the old one.
type grantBindPositions struct {
	totalArgs  int
	scopedIdx  int
	repoIdx    int
	scopeIdx   int
	wantRepos  []string
	wantScopes []string
}

// assertGrantBinds checks the exact shape of the bind arguments rather than
// scanning for "some true bool somewhere". An earlier version of these tests
// did the loose scan and a minimum-length check, which review correctly called
// out: both stay green if the flag binds at the wrong index or the arrays
// arrive nil, which is precisely the bug that would make the predicate inert.
func assertGrantBinds(t *testing.T, args []any, want grantBindPositions) {
	t.Helper()

	if len(args) != want.totalArgs {
		t.Fatalf("bind arg count = %d, want %d; the grant placeholders are positional, so a count change moves them: %#v",
			len(args), want.totalArgs, args)
	}

	scoped, ok := args[want.scopedIdx].(bool)
	if !ok {
		t.Fatalf("arg[%d] = %#v (%T), want a bool scoped flag", want.scopedIdx, args[want.scopedIdx], args[want.scopedIdx])
	}
	if !scoped {
		t.Fatalf("arg[%d] = false, want true; a false flag short-circuits the grant predicate and admits every row", want.scopedIdx)
	}

	wantRepos := pgarray.Array(want.wantRepos)
	if got := args[want.repoIdx]; !reflect.DeepEqual(got, wantRepos) {
		t.Fatalf("arg[%d] = %#v, want %#v; a nil or wrong repository array gives the predicate nothing to match",
			want.repoIdx, got, wantRepos)
	}

	wantScopes := pgarray.Array(want.wantScopes)
	if got := args[want.scopeIdx]; !reflect.DeepEqual(got, wantScopes) {
		t.Fatalf("arg[%d] = %#v, want %#v; a nil or wrong scope array gives the predicate nothing to match",
			want.scopeIdx, got, wantScopes)
	}
}

// TestListGenerationLifecyclePassesGrantToQuery closes the layer a handler
// test cannot reach (#5167 review, codex P1). A test that stops at a recording
// reader proves only that the handler copied grant fields into a filter; it
// still passes if the store drops those arguments on the floor, while a
// generation-id-only request walks straight to another tenant's lifecycle row.
//
// This asserts the values actually arrive as bind arguments, at the positions
// the SQL reads them from. It deliberately does NOT claim to validate the SQL
// predicate itself: that needs a real database with in-grant and out-of-grant
// rows, which this hermetic suite has no engine for, and which must land before
// either route leaves the pending ledger.
func TestListGenerationLifecyclePassesGrantToQuery(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{}}}
	store := StatusStore{queryer: queryer}

	_, err := store.ListGenerationLifecycle(context.Background(), statuspkg.GenerationLifecycleFilter{
		GenerationID:         "someone-elses-generation",
		Scoped:               true,
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
		Limit:                10,
	})
	if err != nil {
		t.Fatalf("ListGenerationLifecycle: %v", err)
	}
	if len(queryer.args) == 0 {
		t.Fatalf("expected the lifecycle query to run")
	}

	// $8/$9/$10 in listGenerationLifecycleQuery.
	assertGrantBinds(t, queryer.args[0], grantBindPositions{
		totalArgs:  10,
		scopedIdx:  7,
		repoIdx:    8,
		scopeIdx:   9,
		wantRepos:  []string{"repo-a"},
		wantScopes: []string{"scope-a"},
	})
}

// TestResolveChangedSinceScopePassesGrantToQuery is the same proof for
// changed-since. Its scope resolution is where the grant binds, so a future
// change that drops these arguments reopens the cross-tenant selector read
// while every handler-level test keeps passing.
func TestResolveChangedSinceScopePassesGrantToQuery(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{}}}
	store := StatusStore{queryer: queryer}

	_, err := store.ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{
		Repository:           "repo-b",
		SinceGenerationID:    "gen-prior",
		Scoped:               true,
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	})
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta: %v", err)
	}
	if len(queryer.args) == 0 {
		t.Fatalf("expected the scope resolution query to run")
	}

	// $3/$4/$5 in resolveChangedSinceScopeQuery.
	assertGrantBinds(t, queryer.args[0], grantBindPositions{
		totalArgs:  5,
		scopedIdx:  2,
		repoIdx:    3,
		scopeIdx:   4,
		wantRepos:  []string{"repo-a"},
		wantScopes: []string{"scope-a"},
	})
}
