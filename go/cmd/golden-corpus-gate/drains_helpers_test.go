// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// drains_helpers_test.go holds the small drain-assertion fixture the I/O-seam
// drain tests share. The pure drain *assertion* logic lives in
// internal/goldengate (and is unit-tested there); this helper only builds the
// strict (zero-tolerance) DrainAssertions the pollUntilDrained seam tests drive.

func int64p(v int64) *int64 { return &v }

// strictDrainAssertions returns a zero-tolerance drain contract: both queues
// must reach a fully terminal state (no residual fact_work_items, no nonterminal
// shared_projection_intents) before pollUntilDrained reports drained.
func strictDrainAssertions() DrainAssertions {
	return DrainAssertions{
		FactWorkItems:           DrainBound{ResidualMax: int64p(0)},
		SharedProjectionIntents: DrainBound{NonterminalMax: int64p(0)},
	}
}
