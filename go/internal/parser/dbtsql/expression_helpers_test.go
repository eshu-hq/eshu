// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dbtsql

import "testing"

// TestReplaceReferenceTokensMatchesWordBoundaries pins replaceReferenceTokens's
// output before and after the per-token regex compile in replaceReferenceTokens
// is replaced by a package-level compiled-pattern cache (issue #4874). The
// cache must not change which occurrences of a reference token are replaced:
// word-boundary semantics (`\bTOKEN\b`) must be preserved exactly, including
// for repeated tokens and tokens that are substrings of other identifiers.
func TestReplaceReferenceTokensMatchesWordBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression string
		references []string
		want       string
	}{
		{
			name:       "single bare identifier replaced",
			expression: "id + 1",
			references: []string{"id"},
			want:       "REF + 1",
		},
		{
			name:       "substring identifier is not replaced",
			expression: "valid_id + id",
			references: []string{"id"},
			want:       "valid_id + REF",
		},
		{
			name:       "qualified reference token replaced as one unit",
			expression: "a.id + b.id",
			references: []string{"a.id"},
			want:       "REF + b.id",
		},
		{
			name:       "repeated token replaced at every occurrence",
			expression: "id + id + id",
			references: []string{"id"},
			want:       "REF + REF + REF",
		},
		{
			name:       "multiple distinct tokens replaced independently",
			expression: "coalesce(a, b)",
			references: []string{"a", "b"},
			want:       "coalesce(REF, REF)",
		},
		{
			name:       "no references leaves expression untouched",
			expression: "1 + 1",
			references: nil,
			want:       "1 + 1",
		},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := replaceReferenceTokens(testCase.expression, testCase.references)
			if got != testCase.want {
				t.Fatalf(
					"replaceReferenceTokens(%q, %v) = %q, want %q",
					testCase.expression, testCase.references, got, testCase.want,
				)
			}
		})
	}
}

// TestReplaceReferenceTokensCacheIsConcurrencySafe exercises
// replaceReferenceTokens from multiple goroutines with overlapping and
// distinct tokens so the -race detector can catch any unsynchronized access
// to the package-level compiled-pattern cache introduced by the regex hoist.
func TestReplaceReferenceTokensCacheIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	tokens := []string{"id", "created_at", "customer_id", "a.id", "b.total"}
	done := make(chan string, len(tokens)*4)
	for i := 0; i < len(tokens)*4; i++ {
		token := tokens[i%len(tokens)]
		go func(token string) {
			done <- replaceReferenceTokens(token+" + 1", []string{token})
		}(token)
	}
	for i := 0; i < len(tokens)*4; i++ {
		if got := <-done; got != "REF + 1" {
			t.Fatalf("replaceReferenceTokens concurrent result = %q, want %q", got, "REF + 1")
		}
	}
}
