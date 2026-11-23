// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestHolderMassProbeQueryIsCappedSingleStatement pins the #10681 probe
// shape: every arm counts through a per-id LATERAL with a LIMIT, the
// per-id total saturates at the cap with LEAST, and the whole probe stays
// one statement. It fails until the cheap-arm rewrite lands.
func TestHolderMassProbeQueryIsCappedSingleStatement(t *testing.T) {
	t.Parallel()

	q := HolderMassProbeQuery
	if strings.Contains(q, ";") {
		t.Fatalf("probe holds more than one statement:\n%s", q)
	}
	for _, want := range []string{"LATERAL", "LIMIT", "LEAST"} {
		if !strings.Contains(q, want) {
			t.Fatalf("probe lost %q:\n%s", want, q)
		}
	}
	if strings.Contains(q, "UNION") {
		t.Fatalf("probe still aggregates arms with UNION instead of per-id LATERALs, leaving an unbounded exact COUNT:\n%s", q)
	}
	if strings.Contains(q, "DISTINCT") {
		t.Fatalf("probe deduplicates matches with DISTINCT, flipping the overestimate bias:\n%s", q)
	}
	if got := strings.Count(q, "LEFT JOIN"); got != 3 {
		t.Fatalf("probe joins %d times, want 3 (one per-id LATERAL arm) with zero-fill", got)
	}
	if got := strings.Count(q, "FROM supply_chain_impact_finding_supports AS support"); got != 3 {
		t.Fatalf("probe reads the supports table %d times, want 3 (one per arm)", got)
	}
	if got := strings.Count(q, "<> ''"); got != 2 {
		t.Fatalf("probe carries the non-empty predicate %d times, want 2 (the cve_id and advisory_id arms prove their partial indexes)", got)
	}
	for _, banned := range []string{"fact_records", facts.ReducerSupplyChainImpactFindingFactKind, "is_tombstone"} {
		if strings.Contains(q, banned) {
			t.Fatalf("probe still reads the finding facts (%q)", banned)
		}
	}
	if !strings.Contains(q, "advisory_group_members") {
		t.Fatalf("probe lost the group-members arm")
	}
	capDigits := strconv.FormatInt(supplychaincore.HolderMassProbeCap, 10)
	if got := strings.Count(q, capDigits); got != 5 {
		t.Fatalf("probe carries the cap %d times, want 5 (LEAST total, three CASE saturation values, already-hot short-circuit)", got)
	}
}

// TestHolderMassProbeCapIsSingleSourced fails a duplicated cap literal: the
// only number in the probe file is the %d verb, and every value flows from
// supplychaincore.HolderMassProbeCap.
func TestHolderMassProbeCapIsSingleSourced(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("supply_chain_impact_fanout_mass.go")
	if err != nil {
		t.Fatalf("read the probe source: %v", err)
	}
	for _, lit := range []string{"10001", "10000"} {
		if strings.Contains(string(body), lit) {
			t.Fatalf("probe source duplicates the %q literal: route the cap through supplychaincore.HolderMassProbeCap", lit)
		}
	}
	if !strings.Contains(string(body), "HolderMassProbeCap") {
		t.Fatalf("probe source no longer references supplychaincore.HolderMassProbeCap")
	}
}
