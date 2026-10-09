// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestProbeIdentityEpochCountsActiveGenerationsOnly pins the #7805 contract at
// the SQL-shape level: the count and max(observed_at) halves of the epoch probe
// must be restricted to each scope's active generation, so retention deletes of
// superseded-generation rows cannot move the epoch of the active identity set.
// TestIdentityEpochIgnoresSupersededGenerationRowsLive proves the behavior
// against real Postgres.
func TestProbeIdentityEpochCountsActiveGenerationsOnly(t *testing.T) {
	t.Parallel()

	factHalf := probeIdentityEpochQuery
	if i := strings.Index(factHalf, "CROSS JOIN"); i >= 0 {
		factHalf = factHalf[:i]
	}
	for _, want := range []string{
		"(fact.scope_id, fact.generation_id) IN (",
		"scope.active_generation_id",
		"generation.status = 'active'",
		"OR FALSE",
	} {
		if !strings.Contains(factHalf, want) {
			t.Fatalf("epoch probe fact half missing %q, so superseded generations still move the epoch:\n%s", want, factHalf)
		}
	}
}
