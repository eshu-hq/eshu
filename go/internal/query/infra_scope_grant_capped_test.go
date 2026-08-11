// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"testing"
)

// TestGrantInlineCapExceededMatchesScalarTruncation is the #5408 regression.
//
// scopeGrantInlineScalars reports capped=true when a scoped token's grant set
// overflows maxScopeGrantInlineTerms, but every call site discards it
// (scalars, _ :=), so an operator has no signal that a token lost USES /
// DEFINES-collision admission for the overflow.
//
// The fix reports the cap from the access filter itself rather than threading
// the discarded bool out of six string-builder call sites. That matters for
// more than tidiness: infraSearchScopeClause alone calls
// scopeGrantInlineScalars three times for one request, so emitting per call
// site would count one capped read three times and make the metric useless for
// "how many reads degraded".
//
// This asserts the reported cap agrees with the truncation that actually
// happens, in both directions. A predicate that answered independently of the
// scalar builder could drift from it silently, and then the metric would
// describe a degradation that did not occur (or miss one that did).
func TestGrantInlineCapExceededMatchesScalarTruncation(t *testing.T) {
	t.Parallel()

	ids := func(prefix string, n int) []string {
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, fmt.Sprintf("%s-%d", prefix, i))
		}
		return out
	}

	for _, tc := range []struct {
		name       string
		allScopes  bool
		repos      []string
		scopes     []string
		wantCapped bool
	}{
		{
			// An all-scopes caller adds no inline-map disjunction at all, so
			// there is nothing to truncate and nothing to report.
			name:      "all-scopes caller is never capped",
			allScopes: true,
			repos:     ids("repo", maxScopeGrantInlineTerms*2),
		},
		{name: "scoped with no grants"},
		{
			name:  "well under the cap",
			repos: ids("repo", 8),
		},
		{
			name:  "exactly at the cap",
			repos: ids("repo", maxScopeGrantInlineTerms),
		},
		{
			name:       "one over the cap",
			repos:      ids("repo", maxScopeGrantInlineTerms+1),
			wantCapped: true,
		},
		{
			// The cap applies to the UNION, so neither set alone reaching it
			// must still report capped once they are combined.
			name:       "union of two under-cap sets crosses it",
			repos:      ids("repo", maxScopeGrantInlineTerms-4),
			scopes:     ids("scope", 16),
			wantCapped: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			filter := repositoryAccessFilter{
				allScopes:            tc.allScopes,
				allowedRepositoryIDs: tc.repos,
				allowedScopeIDs:      tc.scopes,
			}

			got := filter.grantInlineCapExceeded()
			if got != tc.wantCapped {
				t.Fatalf("grantInlineCapExceeded() = %v, want %v", got, tc.wantCapped)
			}

			// The signal must agree with the truncation that actually happens,
			// or the metric reports a degradation unrelated to the query.
			scalars, capped := filter.scopeGrantInlineScalars()
			if capped != got {
				t.Fatalf(
					"grantInlineCapExceeded() = %v but scopeGrantInlineScalars reported capped = %v: "+
						"the operator signal has drifted from the truncation it describes",
					got, capped,
				)
			}
			if got && len(scalars) != maxScopeGrantInlineTerms {
				t.Fatalf(
					"reported capped but returned %d scalars, want exactly %d",
					len(scalars), maxScopeGrantInlineTerms,
				)
			}
		})
	}
}
