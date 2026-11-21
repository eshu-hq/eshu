// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// TestWinnersPayloadColumnsCarried pins #10880 slice 1: the winners
// table carries the winner payload and its source_confidence, the
// winner select projects both from the winning fact (with the
// operator suppression object baked into the payload), and migration
// 325 adds the two columns. Without the data-column entries a
// payload-only change would skip the write-on-change comparison.
func TestWinnersPayloadColumnsCarried(t *testing.T) {
	t.Parallel()

	if !slices.Contains(supplyChainImpactWinnerDataColumns, "payload") {
		t.Fatal("winner data columns omit payload")
	}
	if !slices.Contains(supplyChainImpactWinnerDataColumns, "source_confidence") {
		t.Fatal("winner data columns omit source_confidence")
	}
	for _, want := range []string{
		"AS payload,",
		"source.source_confidence",
		"jsonb_build_object('suppression'",
	} {
		if !strings.Contains(supplyChainImpactWinnerSelectSQL, want) {
			t.Errorf("winner select missing %q", want)
		}
	}
	ddl, err := os.ReadFile("migrations/325_supply_chain_impact_winners_payload.sql")
	if err != nil {
		t.Fatalf("read migration 325: %v", err)
	}
	for _, want := range []string{
		"ADD COLUMN IF NOT EXISTS payload JSONB",
		"ADD COLUMN IF NOT EXISTS source_confidence TEXT",
	} {
		if !strings.Contains(string(ddl), want) {
			t.Errorf("migration 325 missing %q", want)
		}
	}
	if !strings.Contains(stagedStoredWinnersSQL, "environments, payload, source_confidence") {
		t.Error("staged stored-winners read omits payload/source_confidence")
	}
}
