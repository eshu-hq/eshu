// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// TestListGenerationLifecyclePassesGrantToQuery closes the layer a handler
// test cannot reach (#5167 review, codex P1). A test that stops at a recording
// reader proves only that the handler copied grant fields into a filter; it
// still passes if the store drops those arguments on the floor, while a
// generation-id-only request walks straight to another tenant's lifecycle row.
//
// This asserts the values actually arrive as bind arguments. It deliberately
// does NOT claim to validate the SQL predicate itself: that needs a real
// database with in-grant and out-of-grant rows, which this hermetic suite has
// no engine for, and which must land before either route leaves the pending
// ledger.
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

	args := queryer.args[0]
	var sawScoped bool
	for _, a := range args {
		if b, ok := a.(bool); ok && b {
			sawScoped = true
		}
	}
	if !sawScoped {
		t.Fatalf("the scoped flag must reach the query, or the grant predicate is inert; args = %#v", args)
	}
	// Two grant arrays must be bound as well; without them the predicate has
	// nothing to compare against and admits every row.
	if len(args) < 10 {
		t.Fatalf("expected the grant arrays to be bound as additional arguments; got %d args: %#v", len(args), args)
	}
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

	args := queryer.args[0]
	var sawScoped bool
	for _, a := range args {
		if b, ok := a.(bool); ok && b {
			sawScoped = true
		}
	}
	if !sawScoped {
		t.Fatalf("the scoped flag must reach the scope query, or the grant predicate is inert; args = %#v", args)
	}
	if len(args) < 5 {
		t.Fatalf("expected the grant arrays bound as additional arguments; got %d: %#v", len(args), args)
	}
}
