// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"testing"

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
