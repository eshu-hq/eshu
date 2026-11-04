// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

// deltaBaseTestRequests is four delta-active scopes, one per base shape: an
// active base, a superseded base retention pruned, a superseded base with no
// prune event, and no base at the union-base commit. Scope c is chained: its
// union-base commit differs from its fence commit.
func deltaBaseTestRequests() []deltaBaseRequest {
	return []deltaBaseRequest{
		{scopeID: "scope-a", generationID: "delta-a", commits: deltaUnionCommitsFor("commit-a", "")},
		{scopeID: "scope-b", generationID: "delta-b", commits: deltaUnionCommitsFor("commit-b", "")},
		{scopeID: "scope-c", generationID: "delta-c", commits: deltaUnionCommitsFor("commit-c-active", "commit-c")},
		{scopeID: "scope-d", generationID: "delta-d", commits: deltaUnionCommitsFor("commit-d", "")},
	}
}

// TestResolveDeltaBasesBatchesEveryScopeInTwoStatements pins the #10003
// round-trip contract: however many scopes a reader reached, base resolution
// sends one base lookup over every (scope, union-base commit) pair and one
// retention probe over the superseded bases only, and each scope keeps the
// outcome the per-scope rule gave it.
func TestResolveDeltaBasesBatchesEveryScopeInTwoStatements(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{
			{"scope-a", "base-a", "active"},
			{"scope-b", "base-b", "superseded"},
			{"scope-c", "base-c", "superseded"},
		}},
		{rows: [][]any{{retentionHashID("scope", "scope-b")}}},
	}}
	resolved, err := NewFactStore(db).resolveDeltaBases(context.Background(), deltaBaseTestRequests())
	if err != nil {
		t.Fatalf("resolveDeltaBases() error = %v", err)
	}
	if got := len(db.queries); got != 2 {
		t.Fatalf("statements = %d, want 2 (one base lookup, one probe)", got)
	}
	if db.queries[0].query != deltaUnionBaseQuery || db.queries[1].query != deltaUnionBasePrunedQuery {
		t.Fatalf("statements = [%q %q], want the base lookup then the probe", db.queries[0].query, db.queries[1].query)
	}
	wantLookup := []any{
		[]string{"scope-a", "scope-b", "scope-c", "scope-d"},
		[]string{"commit-a", "commit-b", "commit-c", "commit-d"},
	}
	if got := db.queries[0].args; !reflect.DeepEqual(got, wantLookup) {
		t.Fatalf("base lookup args = %#v, want %#v (union-base commits, in request order)", got, wantLookup)
	}
	wantProbe := []any{
		[]string{retentionHashID("scope", "scope-b"), retentionHashID("scope", "scope-c")},
		[]string{retentionHashID("generation", "base-b"), retentionHashID("generation", "base-c")},
	}
	if got := db.queries[1].args; !reflect.DeepEqual(got, wantProbe) {
		t.Fatalf("probe args = %#v, want only the superseded bases %#v", got, wantProbe)
	}
	if got := resolved["scope-a"]; got.baseID != "base-a" || got.refused != nil {
		t.Fatalf("scope-a = %+v, want the active base", got)
	}
	if got := resolved["scope-c"]; got.baseID != "base-c" || got.refused != nil {
		t.Fatalf("scope-c = %+v, want the intact superseded base", got)
	}
	for scopeID, commits := range map[string]deltaUnionCommits{
		"scope-b": deltaUnionCommitsFor("commit-b", ""),
		"scope-d": deltaUnionCommitsFor("commit-d", ""),
	} {
		got := resolved[scopeID].refused
		if got == nil || got.ScopeID != scopeID || got.BaselineCommitSHA != commits.Baseline || got.UnionBaseCommitSHA != commits.UnionBase {
			t.Fatalf("%s refusal = %+v, want a DeltaBaseMissingError naming the scope and both commits", scopeID, got)
		}
		if resolved[scopeID].baseID != "" {
			t.Fatalf("%s base = %q, want none on refusal", scopeID, resolved[scopeID].baseID)
		}
	}
}

// TestResolveDeltaBasesSkipsTheProbeWithoutASupersededBase keeps the
// common case at one statement: active and completed bases are never
// prune-eligible.
func TestResolveDeltaBasesSkipsTheProbeWithoutASupersededBase(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{"scope-a", "base-a", "active"}, {"scope-b", "base-b", "completed"}}},
	}}
	resolved, err := NewFactStore(db).resolveDeltaBases(context.Background(), deltaBaseTestRequests()[:2])
	if err != nil {
		t.Fatalf("resolveDeltaBases() error = %v", err)
	}
	if got := len(db.queries); got != 1 {
		t.Fatalf("statements = %d, want 1 (no probe without a superseded base)", got)
	}
	if resolved["scope-a"].baseID != "base-a" || resolved["scope-b"].baseID != "base-b" {
		t.Fatalf("resolved = %+v, want both bases", resolved)
	}
}

// TestResolveDeltaBasesProbeErrorRefusesOnlySupersededBases keeps #8181 in
// the batched form: a probe failure refuses every superseded base it
// covered, leaves an active base resolved, logs the cause per refused scope,
// and is not an error.
func TestResolveDeltaBasesProbeErrorRefusesOnlySupersededBases(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{"scope-a", "base-a", "active"}, {"scope-b", "base-b", "superseded"}, {"scope-c", "base-c", "superseded"}}},
		{err: errors.New("retention probe outage")},
	}}
	var logs bytes.Buffer
	store := NewFactStore(db).WithLogger(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	resolved, err := store.resolveDeltaBases(context.Background(), deltaBaseTestRequests()[:3])
	if err != nil {
		t.Fatalf("resolveDeltaBases() error = %v, want nil (a probe outage is a refusal)", err)
	}
	if resolved["scope-a"].baseID != "base-a" || resolved["scope-a"].refused != nil {
		t.Fatalf("scope-a = %+v, want the active base kept", resolved["scope-a"])
	}
	for _, scopeID := range []string{"scope-b", "scope-c"} {
		if resolved[scopeID].refused == nil {
			t.Fatalf("%s = %+v, want refused on a probe outage", scopeID, resolved[scopeID])
		}
	}
	if got := strings.Count(logs.String(), "delta union retention probe failed; refusing closed"); got != 2 {
		t.Fatalf("probe-failure debug lines = %d, want one per superseded scope:\n%s", got, logs.String())
	}
}

// TestResolveDeltaBasesFailsOnLookupErrorAndDuplicateScopes keeps a base
// lookup failure an error for the whole call (never a silent refusal), sends
// no statement for an empty request set, and refuses a duplicate scope
// before any statement.
func TestResolveDeltaBasesFailsOnLookupErrorAndDuplicateScopes(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{err: context.DeadlineExceeded}}}
	if _, err := NewFactStore(db).resolveDeltaBases(context.Background(), deltaBaseTestRequests()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lookup failure error = %v, want the deadline error", err)
	}
	empty := &fakeExecQueryer{}
	resolved, err := NewFactStore(empty).resolveDeltaBases(context.Background(), nil)
	if err != nil || len(resolved) != 0 || len(empty.queries) != 0 {
		t.Fatalf("empty requests = %v, %v, %d statements; want no result, no error, no statement", resolved, err, len(empty.queries))
	}
	dup := &fakeExecQueryer{}
	requests := append(deltaBaseTestRequests()[:1], deltaBaseTestRequests()[0])
	if _, err := NewFactStore(dup).resolveDeltaBases(context.Background(), requests); err == nil || len(dup.queries) != 0 {
		t.Fatalf("duplicate scope = %v with %d statements, want an error before any statement", err, len(dup.queries))
	}
}
