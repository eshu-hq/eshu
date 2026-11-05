// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestCrossplaneReaderTextsDeriveDeltaOverlayBase pins that both Crossplane
// SATISFIED_BY reader texts derive their base-candidate text through the
// #10003 overlay helper, each with one scope filter at its own placeholder,
// and that the claim text stays anchored on the intent scope at $1.
func TestCrossplaneReaderTextsDeriveDeltaOverlayBase(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		active string
		param  int
		want   string
	}{
		{"xrd", listActiveCrossplaneXRDFactsQuery, crossplaneXRDDeltaOverlayScopeParam, "AND fact.scope_id = ANY($4::text[])"},
		{"claim", listActiveCrossplaneClaimCandidateFactsQuery, crossplaneClaimDeltaOverlayScopeParam, "AND fact.scope_id = ANY($5::text[])"},
	} {
		derived, err := deriveDeltaOverlayBaseQuery(tc.active, tc.param)
		if err != nil {
			t.Fatalf("%s: derive: %v", tc.name, err)
		}
		if got := strings.Count(derived, tc.want); got != 1 {
			t.Fatalf("%s: derived carries %q %d times, want 1:\n%s", tc.name, tc.want, got, derived)
		}
		if strings.Contains(derived, deltaOverlayActiveScopeJoin) || strings.Contains(derived, deltaOverlayActiveGeneration) {
			t.Fatalf("%s: derived keeps an active-generation fragment:\n%s", tc.name, derived)
		}
	}
	if !strings.Contains(listActiveCrossplaneClaimCandidateFactsQuery, "WHERE fact.scope_id = $1") {
		t.Fatal("claim text is not anchored on the intent scope at $1")
	}
}

// TestListActiveCrossplaneSatisfiedByFactsStatements pins the statements one
// materialization load issues, on one snapshot with one target lookup: with
// no delta-active scope only the two active reads and the lookup run; a base
// read runs only for a leg that can reach a delta-active scope with a
// baseline, and the claim leg only for the intent scope itself.
func TestListActiveCrossplaneSatisfiedByFactsStatements(t *testing.T) {
	t.Parallel()

	claimBase, err := deriveDeltaOverlayBaseQuery(listActiveCrossplaneClaimCandidateFactsQuery, crossplaneClaimDeltaOverlayScopeParam)
	if err != nil {
		t.Fatal(err)
	}
	xrdBase, err := deriveDeltaOverlayBaseQuery(listActiveCrossplaneXRDFactsQuery, crossplaneXRDDeltaOverlayScopeParam)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		targets [][]any
		want    []string
	}{
		{
			name: "no delta-active scope",
			want: []string{listActiveCrossplaneClaimCandidateFactsQuery, deltaOverlayTargetsQuery, listActiveCrossplaneXRDFactsQuery},
		},
		{
			name:    "another scope is delta-active",
			targets: [][]any{{"platform", "platform-delta", "c0", ""}},
			want:    []string{listActiveCrossplaneClaimCandidateFactsQuery, deltaOverlayTargetsQuery, listActiveCrossplaneXRDFactsQuery, xrdBase},
		},
		{
			name:    "the intent scope is delta-active",
			targets: [][]any{{"app", "app-delta", "c0", ""}},
			want:    []string{listActiveCrossplaneClaimCandidateFactsQuery, deltaOverlayTargetsQuery, claimBase, listActiveCrossplaneXRDFactsQuery, xrdBase},
		},
		{
			name:    "a legacy delta has no baseline",
			targets: [][]any{{"app", "app-delta", "", ""}},
			want:    []string{listActiveCrossplaneClaimCandidateFactsQuery, deltaOverlayTargetsQuery, listActiveCrossplaneXRDFactsQuery},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			responses := []queueFakeRows{{}, {rows: tc.targets}}
			for range len(tc.want) - 2 {
				responses = append(responses, queueFakeRows{})
			}
			db := &fakeExecQueryer{queryResponses: responses}
			loaded, err := NewFactStore(db).ListActiveCrossplaneSatisfiedByFacts(context.Background(), "app")
			if err != nil {
				t.Fatalf("ListActiveCrossplaneSatisfiedByFacts() error = %v", err)
			}
			if len(loaded) != 0 {
				t.Fatalf("loaded = %d rows, want 0 from empty pages", len(loaded))
			}
			got := make([]string, 0, len(db.queries))
			for _, call := range db.queries {
				got = append(got, call.query)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("statements = %d %q, want %d", len(got), got, len(tc.want))
			}
			if db.beginReadOnlyRepeatableReadCalls != 1 {
				t.Fatalf("BEGIN = %d, want one snapshot for both legs", db.beginReadOnlyRepeatableReadCalls)
			}
			// The claim texts run custom plans (#10005): under the pool's
			// cached statements PostgreSQL switched the claim base text to a
			// generic plan from its sixth execution, 275 ms against 16 ms on
			// the shim's 50k-file scope. The XRD texts keep their generic
			// plan, which stays on the migration 279 index.
			for i, call := range db.queries {
				isClaim := call.query == listActiveCrossplaneClaimCandidateFactsQuery || call.query == claimBase
				custom := call.hasExecMode && call.execMode == pgx.QueryExecModeCacheDescribe
				if isClaim != custom {
					t.Fatalf("statement %d exec mode = %v (set %v); want QueryExecModeCacheDescribe on the claim texts only", i+1, call.execMode, call.hasExecMode)
				}
			}
			if tc.name == "the intent scope is delta-active" {
				if scopes, _ := db.queries[2].args[4].([]string); !slices.Equal(scopes, []string{"app"}) {
					t.Fatalf("claim base scope ids = %v, want [app]", db.queries[2].args[4])
				}
			}
		})
	}
}
