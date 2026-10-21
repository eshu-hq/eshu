// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/retirement"
)

// TestRetirementCensusStaleSkipsInvisibleRows pins the flag contract: a
// disposition row the scan cannot produce is skipped by InvisibleToScan,
// never by prose-matching its Reason.
func TestRetirementCensusStaleSkipsInvisibleRows(t *testing.T) {
	t.Parallel()

	dispositions := map[string]retirement.RetireTableEntry{
		"seen":      {Table: "seen"},
		"gone":      {Table: "gone"},
		"invisible": {Table: "invisible", Reason: "no matching column so the schema scan cannot see it", InvisibleToScan: true},
	}
	rolled := map[string]*RetirementCensusHit{
		"seen": {Table: "seen"},
	}
	stale := retirementCensusStale(rolled, dispositions)
	if len(stale) != 1 || stale[0] != "gone" {
		t.Fatalf("stale = %v, want [gone]", stale)
	}
}

// TestRetirementCensusInvisibleFlagMatchesProse pins exactness on the real
// table: every flagged row says the scan cannot see it, and no unflagged
// row carries that prose (otherwise the flag and the Reason disagree about
// what the scan produces).
func TestRetirementCensusInvisibleFlagMatchesProse(t *testing.T) {
	t.Parallel()

	var flagged []string
	for table, entry := range retirement.RetireTableDispositions {
		saysInvisible := strings.Contains(entry.Reason, "the schema scan cannot see it")
		if entry.InvisibleToScan {
			flagged = append(flagged, table)
		}
		if entry.InvisibleToScan && !saysInvisible {
			t.Errorf("%s: InvisibleToScan without the prose that justifies it", table)
		}
		if !entry.InvisibleToScan && saysInvisible {
			t.Errorf("%s: prose says the scan cannot see it but InvisibleToScan is false", table)
		}
	}
	if len(flagged) != 3 {
		t.Errorf("flagged rows = %v, want exactly the 3 scan-invisible dispositions", flagged)
	}
}
