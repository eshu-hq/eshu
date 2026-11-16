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
// shape: cheap arms count exactly, the group-members arm counts through a
// per-id LATERAL with a LIMIT, the per-id total saturates at the cap with
// LEAST, and the whole probe stays one statement. It fails until the rewrite
// lands.
func TestHolderMassProbeQueryIsCappedSingleStatement(t *testing.T) {
	t.Parallel()

	q := HolderMassProbeQuery
	if strings.Contains(q, ";") {
		t.Fatalf("probe holds more than one statement:\n%s", q)
	}
	for _, want := range []string{"LATERAL", "LIMIT", "LEAST", "UNION ALL"} {
		if !strings.Contains(q, want) {
			t.Fatalf("probe lost %q:\n%s", want, q)
		}
	}
	if withoutAll := strings.ReplaceAll(q, "UNION ALL", ""); strings.Contains(withoutAll, "UNION") {
		t.Fatalf("probe deduplicates arms with UNION instead of UNION ALL, flipping the overestimate bias:\n%s", q)
	}
	if strings.Contains(q, "DISTINCT") {
		t.Fatalf("probe deduplicates matches with DISTINCT, flipping the overestimate bias:\n%s", q)
	}
	if got := strings.Count(q, "LEFT JOIN"); got != 2 {
		t.Fatalf("probe joins %d times, want 2 (cheap-arm aggregate plus the conditional member arm) with zero-fill", got)
	}
	if got := strings.Count(q, "is_tombstone = FALSE"); got != 3 {
		t.Fatalf("probe carries the tombstone predicate %d times, want 3 (one per arm, proving the partial-index predicates)", got)
	}
	if !strings.Contains(q, facts.ReducerSupplyChainImpactFindingFactKind) {
		t.Fatalf("probe lost the literal fact kind")
	}
	if !strings.Contains(q, "advisory_group_members") {
		t.Fatalf("probe lost the group-members arm")
	}
	capDigits := strconv.FormatInt(supplychaincore.HolderMassProbeCap, 10)
	if got := strings.Count(q, capDigits); got != 3 {
		t.Fatalf("probe carries the cap %d times, want 3 (LEAST total, CASE saturation value, already-hot short-circuit)", got)
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
