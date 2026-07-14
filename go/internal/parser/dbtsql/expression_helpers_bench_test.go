// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dbtsql

import (
	"regexp"
	"testing"
)

// replaceReferenceTokensOldCompilePerCall reproduces replaceReferenceTokens's
// pre-hoist behavior (issue #4874): it compiled a `\bTOKEN\b` regex for every
// reference token on every call instead of reusing a package-level cache. It
// exists only as the "OLD" side of the
// BenchmarkReplaceReferenceTokensRegexHoist before/after comparison;
// production code always calls the cached replaceReferenceTokens.
func replaceReferenceTokensOldCompilePerCall(expression string, references []string) string {
	sanitized := expression
	for _, token := range references {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(token) + `\b`)
		sanitized = re.ReplaceAllString(sanitized, "REF")
	}
	return sanitized
}

// commonDBTReferenceTokens simulates the realistic case the cache targets:
// dbt SQL expressions across a project overwhelmingly reuse a small set of
// column/alias names ("id", "created_at", ...), so replaceReferenceTokens is
// called with the same tokens far more often than with novel ones.
var commonDBTReferenceTokens = []string{"id", "customer_id", "created_at", "updated_at", "a.total"}

// BenchmarkReplaceReferenceTokensRegexHoist is the Prove-The-Theory-First /
// structural-certain evidence for issue #4874's dbtsql regex hoist. The
// RepeatedTokens sub-benchmarks are the representative case (common
// column/alias names reused across many expressions in a real dbt project);
// the UniqueTokensPerCall sub-benchmarks are the honest worst case for the
// cache (every token is novel, so every call is a cache miss) reported for
// transparency per the repo's Evidence Rules.
func BenchmarkReplaceReferenceTokensRegexHoist(b *testing.B) {
	expression := "coalesce(id, customer_id) + created_at - updated_at + a.total"

	b.Run("RepeatedTokens/Old_CompilePerCall", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = replaceReferenceTokensOldCompilePerCall(expression, commonDBTReferenceTokens)
		}
	})

	b.Run("RepeatedTokens/New_CachedPattern", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = replaceReferenceTokens(expression, commonDBTReferenceTokens)
		}
	})

	b.Run("UniqueTokensPerCall/Old_CompilePerCall", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			token := uniqueTokenForIteration(i)
			_ = replaceReferenceTokensOldCompilePerCall(expression, []string{token})
		}
	})

	b.Run("UniqueTokensPerCall/New_CachedPattern", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			token := uniqueTokenForIteration(i)
			_ = replaceReferenceTokens(expression, []string{token})
		}
	})
}

func uniqueTokenForIteration(i int) string {
	digits := make([]byte, 0, 12)
	if i == 0 {
		digits = append(digits, '0')
	}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return "col_" + string(digits)
}
