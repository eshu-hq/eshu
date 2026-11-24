// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestWinnersKeyedActiveFactsProbesLiveGensOncePerTick pins the per-tick
// liveness shape (#10839): the active-generation check must run once per
// distinct live (scope_id, generation_id) pair, never once per candidate
// copy. The shipped shape joined ingestion_scopes and scope_generations per
// wide fact row (50,123 probes for 236 distinct pairs on the QA replica,
// ~622k buffers at ~10x the fact fetch), so per-tick cost scaled with
// copies-per-identifier instead of identifiers. The fixed shape precomputes
// the live generation set once per tick and semi-joins the narrow probe to
// it. The old shape has no live_gens reference at all, so this fails on the
// pre-fix tree.
func TestWinnersKeyedActiveFactsProbesLiveGensOncePerTick(t *testing.T) {
	t.Parallel()

	arm := buildWinnersKeyedActiveFactsSQL
	if !strings.Contains(arm, "live_gens AS MATERIALIZED") {
		t.Error("active_facts build does not precompute the once-per-tick live generation set: per-copy liveness probes are back")
	}
	if !strings.Contains(arm, "JOIN live_gens AS g") {
		t.Error("active_facts build does not semi-join the narrow probe to the live generation set: per-copy liveness probes are back")
	}
	if strings.Contains(arm, "scope.scope_id = fact.scope_id") {
		t.Error("active_facts build still joins liveness per wide fact row: the per-copy probe shape is back")
	}
	if strings.Contains(arm, "generation.scope_id = fact.scope_id") {
		t.Error("active_facts build still joins generations per wide fact row: the per-copy probe shape is back")
	}
}
