// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestProbeIdentityEpochQueryUsesGroupedPairAggregation locks the #8875
// epoch-probe reformulation (arbiter ruling, option (a)): the digest stays a
// byte-identical reformulation of the current fact-derived pair set — one
// scan grouped by (scope_id, generation_id) carrying per-group count and
// max, with the outer aggregate carrying SUM/MAX plus the ordered
// string_agg over the pair strings (DISTINCT kept, ordered by the
// concatenated scope:generation text). No digest algorithm change, no
// empty-count term, no memoization, detection stays on the probe path.
//
// This is a pure SQL-shape assertion (no DB required); the byte-identity of
// the whole epoch triple is proven against real Postgres by
// TestIdentityEpochProbeReformulationMatchesOracleLive.
func TestProbeIdentityEpochQueryUsesGroupedPairAggregation(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"GROUP BY fact.scope_id, fact.generation_id",
		"SUM(fact.cnt)",
		"MAX(fact.max_observed)",
		"COUNT(*)",
		"MAX(fact.observed_at)",
	} {
		if !strings.Contains(probeIdentityEpochQuery, want) {
			t.Fatalf("probeIdentityEpochQuery missing grouped reformulation piece %q:\n%s", want, probeIdentityEpochQuery)
		}
	}
}
