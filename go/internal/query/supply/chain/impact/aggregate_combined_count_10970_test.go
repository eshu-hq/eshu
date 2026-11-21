// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var placeholderPattern = regexp.MustCompile(`\$(\d+)`)

// TestAggregateCountSingleStatement10970 is the TDD failing test for #10970:
// the count envelope must answer in one Postgres statement (one CTE build,
// one DISTINCT ON sort), not three sequential full-CTE statements.
func TestAggregateCountSingleStatement10970(t *testing.T) {
	t.Parallel()

	if !strings.Contains(AggregateCombinedCountQuery, "canonical_facts") {
		t.Fatalf("combined count query must read canonical_facts")
	}
	for _, needle := range []string{
		"COALESCE(NULLIF(fact.priority_bucket, ''), 'unknown')",
		"COALESCE(NULLIF(fact.severity_bucket, ''), 'none')",
	} {
		if !strings.Contains(AggregateCombinedCountQuery, needle) {
			t.Fatalf("combined count query missing bucket expression %q", needle)
		}
	}
	// The combined shape must keep the 26 positional parameters of the three
	// existing count queries (no new placeholders).
	for _, needle := range []string{"$1", "$14", "$20::timestamptz", "$26"} {
		if !strings.Contains(AggregateCombinedCountQuery, needle) {
			t.Fatalf("combined count query missing placeholder %q", needle)
		}
	}
	highest := 0
	for _, m := range placeholderPattern.FindAllStringSubmatch(AggregateCombinedCountQuery, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("placeholder $%s is not numeric: %v", m[1], err)
		}
		if n > highest {
			highest = n
		}
	}
	if highest != 26 {
		t.Fatalf("combined count query highest placeholder = $%d, want $26 (same 26-arg contract as the facet queries)", highest)
	}
}
