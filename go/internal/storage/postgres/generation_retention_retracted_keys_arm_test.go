// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestRetentionRowCountsCountTheRetractedKeysTable pins that the composed
// row-count statement counts supply_chain_impact_retracted_finding_keys: the
// table's generation foreign key cascades, so a generation delete removes its
// rows and the retention report must charge them (#10880 PR-C1 c). The arm
// probes the primary key's leading generation_id column and groups by candidate
// generation like the supports arm.
func TestRetentionRowCountsCountTheRetractedKeysTable(t *testing.T) {
	t.Parallel()

	query := generationRetentionRowCountsQuery
	if !strings.Contains(query, generationRetentionRowCountsRetractedKeysArm) {
		t.Fatal("the composed row-count statement does not include the retracted-keys arm")
	}
	for _, want := range []string{
		"'supply_chain_impact_retracted_finding_keys' AS table_name",
		"LEFT JOIN supply_chain_impact_retracted_finding_keys AS retracted",
		"ON retracted.generation_id = candidate.generation_id",
	} {
		if !strings.Contains(generationRetentionRowCountsRetractedKeysArm, want) {
			t.Errorf("the retracted-keys arm lacks %q", want)
		}
	}
	if got := strings.Count(query, "supply_chain_impact_retracted_finding_keys"); got != 2 {
		t.Errorf("the composed statement names the retracted-keys table %d times, want 2 (the label and the join)", got)
	}
}
