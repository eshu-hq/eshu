// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reachabilitystore

import (
	"regexp"
	"strings"
	"testing"
)

// TestPendingReachabilityLoaderLimitStaysParameterized is the #7596 item-4
// regression guard.
//
// Item 4's leftover from #7579 was whether the loader behaves differently at
// the production LIMIT 100 (measured at LIMIT 10). The drive shim's EXPLAIN
// matrix (custom vs generic plan across LIMIT 10 vs 100) came back identical
// except one const-folded cosmetic line, cost 34.97-34.98 in all four cells:
// LIMIT is plan-neutral here because it is a bound parameter ($1) on a
// statement whose estimated candidate row count sits below both limits, and
// the generic plan after the fifth execution sees the same shape. That only
// holds while the limit stays a bind parameter: interpolating it as a literal
// (LIMIT 10 / LIMIT 100) would bake a per-limit plan and invalidate the
// matrix. This test pins the textual preconditions on the production
// statement: exactly one LIMIT clause, and it is LIMIT $1; the only other
// bind parameter is the verdict epoch ($2), so a forced generic plan still
// covers the whole statement.
//
// The deterministic ORDER BY + LIMIT prefix property (LIMIT 10 returns the
// first 10 rows of LIMIT 100) is already pinned by
// TestCodeReachabilityPendingInputsWatchAllTraversedDomains; this test does
// not repeat it.
func TestPendingReachabilityLoaderLimitStaysParameterized(t *testing.T) {
	query := listPendingCodeReachabilityInputsSQL
	uncommented := regexp.MustCompile(`(?m)--.*$`).ReplaceAllString(query, "")
	if limits := regexp.MustCompile(`(?i)\bLIMIT\b`).FindAllString(uncommented, -1); len(limits) != 1 {
		t.Fatalf("pending reachability query has %d LIMIT clauses, want exactly 1:\n%s", len(limits), query)
	}
	if !strings.Contains(uncommented, "LIMIT $1") {
		t.Fatalf("pending reachability query LIMIT is not the bound batch parameter (LIMIT $1):\n%s", query)
	}
	if regexp.MustCompile(`(?i)\bLIMIT\s+[0-9]`).MatchString(uncommented) {
		t.Fatalf("pending reachability query interpolates a literal LIMIT (plan varies per limit):\n%s", query)
	}
	if regexp.MustCompile(`\$3\b`).MatchString(uncommented) {
		t.Fatalf("pending reachability query gained a third bind parameter (generic-plan scope changed):\n%s", query)
	}
}
