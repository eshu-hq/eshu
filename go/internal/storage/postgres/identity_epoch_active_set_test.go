// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestProbeIdentityEpochCountsActiveGenerationsOnly pins the #7805 contract at
// the SQL-shape level: the epoch probe must restrict its rows to each scope's
// active generation, so retention deletes of superseded-generation rows cannot
// move the epoch of the active identity set. Since #7825 the probe is one
// single scan (count, max and fingerprint over the same filtered fact rows),
// so the pins apply to the whole query.
// TestIdentityEpochIgnoresSupersededGenerationRowsLive proves the behavior
// against real Postgres.
func TestProbeIdentityEpochCountsActiveGenerationsOnly(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"(fact.scope_id, fact.generation_id) IN (",
		"scope.active_generation_id",
		"generation.status = 'active'",
		"OR FALSE",
	} {
		if !strings.Contains(probeIdentityEpochQuery, want) {
			t.Fatalf("epoch probe missing %q, so superseded generations still move the epoch:\n%s", want, probeIdentityEpochQuery)
		}
	}
}
