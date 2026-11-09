// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
)

// TestRFC3339PatternParityWithAdvisoryNewestCopy pins the duplicated strict
// RFC 3339 pattern: the advisory newest-copy election
// (advisory.AdvisoryGHSARFC3339Pattern, ranking modified_at) must stay
// behavior-identical to the impact reader's readinessRFC3339Pattern
// (matching covered_through), or a later edit to either copy silently breaks
// election parity with no failing check (#10467 review).
func TestRFC3339PatternParityWithAdvisoryNewestCopy(t *testing.T) {
	if advisory.AdvisoryGHSARFC3339Pattern != readinessRFC3339Pattern {
		t.Fatalf("advisory newest-copy RFC3339 pattern diverged from the impact reader:\n advisory: %q\n impact:   %q",
			advisory.AdvisoryGHSARFC3339Pattern, readinessRFC3339Pattern)
	}
}
