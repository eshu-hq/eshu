// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package preparse

import "testing"

// BenchmarkDecide measures the pre-parse decision on the two inputs the
// #8095 no-regression evidence cites: an eligible skip and a stampable
// first sweep. Both must stay orders of magnitude below the guarded git status
// subprocess (milliseconds) the decision gates, so the opt-in setting costs
// nothing measurable when it parses as before.
func BenchmarkDecide(b *testing.B) {
	skip := eligibleInput()
	stampable := eligibleInput()
	stampable.Active.Identity = ""
	stampable.Identity = "preparse:v1:now"
	stampable.StampWithoutParse = true
	stampable.StampProven = true
	b.Run("eligible-skip", func(b *testing.B) {
		for range b.N {
			if got := Decide(skip, clean); got != OutcomeSkipped {
				b.Fatalf("Decide() = %q, want skipped", got)
			}
		}
	})
	b.Run("stampable", func(b *testing.B) {
		for range b.N {
			if got := Decide(stampable, clean); got != OutcomeStamped {
				b.Fatalf("Decide() = %q, want stamped_without_parse", got)
			}
		}
	})
}
