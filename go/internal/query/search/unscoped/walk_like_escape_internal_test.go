// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestWalkBindsEscapedLiteralWhileClassifyingRaw pins the #10604 (fix 05b)
// seam: the walk's SQL binds carry the pre-escaped LIKE literal, while
// trigram classification, telemetry, and the step/tail choice keep reading
// the caller's raw pattern. Escaping inserts a backslash only before a
// non-alphanumeric, so it can never split an alphanumeric run: classification
// of the raw form always equals classification of the bound form, which the
// table below asserts per pattern alongside the bind.
func TestWalkBindsEscapedLiteralWhileClassifyingRaw(t *testing.T) {
	t.Parallel()

	patterns := []string{
		"handler",
		"han_ler",
		"100%",
		"100%coverage",
		"%",
		"_",
		`\`,
		`trail\`,
		`fmt.Sprintf("%d", a_b)`,
		"naïveté",
		"naïveté_100%",
		"a%b",
	}
	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()

			w := &walk[querycontract.FileContent, querycontract.SearchCursor]{
				shape:   &fileShape,
				pattern: pattern,
				want:    10,
			}
			// The walk stores the caller's raw string untouched.
			if w.pattern != pattern {
				t.Fatalf("walk pattern = %q, want raw %q", w.pattern, pattern)
			}
			// Classification on the raw form equals classification of the
			// bound form: the escape cannot move the tail-skip decision.
			if got, want := patternHasTrigram(w.pattern), patternHasTrigram(querycontract.EscapeLikeLiteral(pattern)); got != want {
				t.Errorf("patternHasTrigram(raw %q) = %v, escaped form = %v, want equal", pattern, got, want)
			}
			// Both SQL binds carry the escaped literal, never the raw pattern.
			want := querycontract.EscapeLikeLiteral(pattern)
			if _, args := w.stepStatement(200); args[1] != want {
				t.Errorf("step bind = %q, want escaped %q", args[1], want)
			}
			if _, args := w.tailStatement(); args[1] != want {
				t.Errorf("tail bind = %q, want escaped %q", args[1], want)
			}
		})
	}
}
