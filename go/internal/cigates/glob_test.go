// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestMatchGlob(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		// Literal matches
		{name: "exact match", pattern: "go/internal/foo/bar.go", path: "go/internal/foo/bar.go", want: true},
		{name: "exact no match", pattern: "go/internal/foo/bar.go", path: "go/internal/foo/baz.go", want: false},

		// Single-star within segment
		{name: "star in segment matches", pattern: "go/internal/*/bar.go", path: "go/internal/foo/bar.go", want: true},
		{name: "star does not cross segment", pattern: "go/internal/*/bar.go", path: "go/internal/foo/sub/bar.go", want: false},
		{name: "star matches multiple chars in segment", pattern: "go/internal/foo/*.go", path: "go/internal/foo/some_file.go", want: true},
		{name: "star prefix no match different ext", pattern: "go/internal/foo/*.go", path: "go/internal/foo/file.ts", want: false},

		// Double-star across segments
		{name: "doublestar matches zero segments", pattern: "go/**/*.go", path: "go/foo.go", want: true},
		{name: "doublestar matches one segment", pattern: "go/**/*.go", path: "go/internal/foo.go", want: true},
		{name: "doublestar matches two segments", pattern: "go/**/*.go", path: "go/internal/foo/bar.go", want: true},
		{name: "doublestar matches many segments", pattern: "go/**/*.go", path: "go/a/b/c/d/e.go", want: true},
		{name: "doublestar no match wrong ext", pattern: "go/**/*.go", path: "go/internal/foo.ts", want: false},
		{name: "doublestar at end matches all", pattern: "go/internal/**", path: "go/internal/foo/bar.go", want: true},
		{name: "doublestar at end matches direct child", pattern: "go/internal/**", path: "go/internal/foo.go", want: true},
		{name: "doublestar at end no match outside", pattern: "go/internal/**", path: "go/cmd/foo.go", want: false},
		{name: "doublestar in middle", pattern: "go/**/openapi*.go", path: "go/internal/query/openapi_gen.go", want: true},
		{name: "doublestar in middle many levels", pattern: "go/**/openapi*.go", path: "go/a/b/c/openapi_thing.go", want: true},
		{name: "doublestar in middle no match", pattern: "go/**/openapi*.go", path: "go/internal/query/handler.go", want: false},

		// Anchor semantics — no leading slash
		{name: "leading slash pattern", pattern: "/go/internal/foo.go", path: "go/internal/foo.go", want: false},

		// Trailing slash
		{name: "trailing slash pattern", pattern: "go/internal/", path: "go/internal/foo.go", want: false},

		// Empty inputs
		{name: "empty pattern", pattern: "", path: "go/foo.go", want: false},
		{name: "empty path", pattern: "go/foo.go", path: "", want: false},
		{name: "both empty", pattern: "", path: "", want: true},

		// Pattern with doublestar only
		{name: "pure doublestar matches everything", pattern: "**", path: "go/internal/foo.go", want: true},
		{name: "pure star matches single segment", pattern: "*", path: "foo", want: true},
		{name: "pure star does not match segment with slash", pattern: "*", path: "go/foo", want: false},

		// Realistic registry patterns
		{name: "openapi pattern", pattern: "go/internal/query/openapi*.go", path: "go/internal/query/openapi_handler.go", want: true},
		{name: "specs surface-inventory", pattern: "specs/surface-inventory.v1.yaml", path: "specs/surface-inventory.v1.yaml", want: true},
		{name: "schema cypher wildcard", pattern: "go/internal/storage/cypher/**", path: "go/internal/storage/cypher/write.go", want: true},
		{name: "docs wildcard", pattern: "docs/**", path: "docs/public/reference/local-testing.md", want: true},
		{name: "src wildcard", pattern: "src/**", path: "src/components/App.tsx", want: true},
		{name: "apps console wildcard", pattern: "apps/console/**", path: "apps/console/src/index.ts", want: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cigates.MatchGlob(tc.pattern, tc.path)
			if got != tc.want {
				t.Errorf("MatchGlob(%q, %q) = %v; want %v", tc.pattern, tc.path, got, tc.want)
			}
		})
	}
}

// TestMatchGlob_NoMatchIsBoundedOnAdversarialPattern pins the COST of
// answering "no match", not the answer. MatchGlob is what Select and
// UncoveredPaths call per (trigger, changed path) pair and what Validate's
// trigger resolution shares a matcher with, so a pattern that blows up here
// stalls gate selection itself.
//
// The matcher branches at every "**" segment, so two or more of them let a
// naive recursion re-explore the same (pattern suffix, path suffix) states
// exponentially — on a pattern that is legal and simply matches nothing.
// Measured on this repository, memoized against the same call forced
// unmemoized, growing the pattern one segment at a time:
//
//	consecutive "**"           n=14   63.2µs   vs  554.7ms
//	consecutive "**"           n=17   11.2µs   vs   24.7s
//	"**" separated by literals n=12    4.9µs   vs  261.4ms
//	"**" separated by literals n=15    3.8µs   vs   17.4s
//
// The fixtures are the n=17 and n=15 rows, and the budget is 2s: the memoized
// answer arrives ~200,000x inside it, while removing the memo needs a machine
// 12x (or 8.7x) faster than the one measured on before the case would pass
// anyway. Anything tighter would flake; anything looser stops separating them.
// A 2s budget over the n=14 row does NOT separate them — 554ms passes — which
// is why the sizes are load-bearing rather than arbitrary.
//
// The second case is what rules out the cheaper fix codex offered alongside
// memoization. Collapsing consecutive "**" into one answers the first case and
// leaves this one untouched, because these "**" segments are separated by
// literals and never adjacent.
func TestMatchGlob_NoMatchIsBoundedOnAdversarialPattern(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		pattern string
		path    string
	}{
		{
			name:    "consecutive double stars",
			pattern: strings.Repeat("**/", 17) + "zzz-no-such-literal",
			path:    strings.TrimSuffix(strings.Repeat("s/", 18), "/"),
		},
		{
			name:    "double stars separated by literals",
			pattern: strings.Repeat("**/s/", 15) + "zzz-no-such-literal",
			path:    strings.TrimSuffix(strings.Repeat("s/", 32), "/"),
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const budget = 2 * time.Second
			done := make(chan bool, 1)
			go func() { done <- cigates.MatchGlob(tc.pattern, tc.path) }()
			select {
			case got := <-done:
				if got {
					t.Fatalf("MatchGlob(%q, %q) = true; want false — the fixture must be a NO-match, or it proves nothing about the cost of answering one", tc.pattern, tc.path)
				}
			case <-time.After(budget):
				t.Fatalf("MatchGlob(%q, %q) did not answer within %v — the exponential \"**\" backtracking is back", tc.pattern, tc.path, budget)
			}
		})
	}
}
