// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import "testing"

// BenchmarkEvaluateQAFixtureSteadyState measures one steady-state cycle over
// the 802-scope QA corpus with a full prior projection, the per-cycle cost
// shard 0 pays in githubOrg mode before its single upsert.
func BenchmarkEvaluateQAFixtureSteadyState(b *testing.B) {
	known, listing := qaFixture()
	first := Evaluate(Input{
		Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval,
		Listing: listing, Known: known,
	})
	input := Input{
		Selector: testSelector, Now: cycleOne.Add(minimumInterval), MinimumInterval: minimumInterval,
		Listing: listing, Known: known, Prior: first.Projected,
	}
	b.ReportAllocs()
	for b.Loop() {
		if result := Evaluate(input); result.Outcome != OutcomeEvaluated {
			b.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeEvaluated)
		}
	}
}
