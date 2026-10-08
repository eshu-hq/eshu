// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
)

// BenchmarkEvaluateQAFixtureSteadyState measures one steady-state cycle over
// the 802-scope QA corpus with a full prior projection, the per-cycle cost
// shard 0 pays in githubOrg mode before its single upsert.
func BenchmarkEvaluateQAFixtureSteadyState(b *testing.B) {
	known, listing := qaFixture()
	first := evaluateAt(cycleOne, known, listing, nil)
	next := cycleOne.Add(selection.ConfirmationMinSpan)
	b.ReportAllocs()
	for b.Loop() {
		if result := evaluateAt(next, known, listing, first.Projected); result.Outcome != OutcomeEvaluated {
			b.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeEvaluated)
		}
	}
}
