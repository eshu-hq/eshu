// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"strconv"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestHolderTrueMassQueryIsGateBound pins the #10972 H2 A2 exact-count
// shape: the same three per-id LATERAL arms as the probe, every arm counting
// over LIMIT gate+2 and reporting gate+1 above gate+1, so the report is exact
// at or under the key-range gate and saturates above it. The report feeds
// only the above-gate routing decision, so saturation high is
// decision-exact — and it bounds each arm's heap fetch, which an uncapped
// COUNT would leave open on member-heavy ids.
func TestHolderTrueMassQueryIsGateBound(t *testing.T) {
	t.Parallel()

	q := HolderTrueMassQuery
	gate := int64(supplychaincore.HolderKeyRangeMassGate)
	bound := gate + 1
	boundDigits := strconv.FormatInt(bound, 10)

	if strings.Contains(q, ";") {
		t.Fatalf("true-mass count holds more than one statement:\n%s", q)
	}
	for _, want := range []string{"LATERAL", "LIMIT", "LEAST"} {
		if !strings.Contains(q, want) {
			t.Fatalf("true-mass count lost %q:\n%s", want, q)
		}
	}
	if strings.Contains(q, "UNION") {
		t.Fatalf("true-mass count aggregates arms with UNION instead of per-id LATERALs:\n%s", q)
	}
	if strings.Contains(q, "DISTINCT") {
		t.Fatalf("true-mass count deduplicates matches with DISTINCT, flipping the overestimate bias:\n%s", q)
	}
	if strings.Contains(q, "EXISTS") {
		t.Fatalf("true-mass count uses the rejected EXISTS-to-cap shape:\n%s", q)
	}
	if got := strings.Count(q, "LEFT JOIN"); got != 3 {
		t.Fatalf("true-mass count joins %d times, want 3 (one per-id LATERAL arm) with zero-fill", got)
	}
	if got := strings.Count(q, "FROM supply_chain_impact_finding_supports AS support"); got != 3 {
		t.Fatalf("true-mass count reads the supports table %d times, want 3 (one per arm)", got)
	}
	if got := strings.Count(q, "<> ''"); got != 2 {
		t.Fatalf("true-mass count carries the non-empty predicate %d times, want 2 (the cve_id and advisory_id arms prove their partial indexes)", got)
	}
	if !strings.Contains(q, "advisory_group_members") {
		t.Fatalf("true-mass count lost the group-members arm")
	}
	// Every arm saturates at the bound above the gate: exact at or under the
	// gate, decision-exact above it.
	fillVerb := "CASE WHEN COUNT(*) > " + boundDigits +
		" THEN " + boundDigits + " ELSE COUNT(*) END"
	if got := strings.Count(q, fillVerb); got != 3 {
		t.Fatalf("true-mass count carries %d gate-bound saturation verbs, want 3 (one per arm):\n%s", got, q)
	}
	limitVerb := "LIMIT " + strconv.FormatInt(bound+1, 10)
	if got := strings.Count(q, limitVerb); got != 3 {
		t.Fatalf("true-mass count carries %d bounded fetches %q, want 3 (one per arm):\n%s", got, limitVerb, q)
	}
	// The bound reads 8 times: the LEAST total, three arm comparisons, three
	// CASE saturation values, and the already-hot short-circuit. (The probe
	// reads its cap 5 times only because its fill threshold differs from its
	// cap; here both bounds are one row over the gate.)
	if got := strings.Count(q, boundDigits); got != 8 {
		t.Fatalf("true-mass count carries the bound %d times, want 8 (LEAST total, three comparisons, three CASE saturation values, already-hot short-circuit)", got)
	}
	for _, banned := range []string{"fact_records", "is_tombstone"} {
		if strings.Contains(q, banned) {
			t.Fatalf("true-mass count still reads the finding facts (%q)", banned)
		}
	}
}

// TestHolderTrueMassBoundIsSingleSourced fails a duplicated gate literal:
// the only numbers in the mass file are the %d verbs, and every value flows
// from supplychaincore.HolderKeyRangeMassGate.
func TestHolderTrueMassBoundIsSingleSourced(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("supply_chain_impact_fanout_mass.go")
	if err != nil {
		t.Fatalf("read the mass source: %v", err)
	}
	gateDigits := strconv.FormatInt(int64(supplychaincore.HolderKeyRangeMassGate), 10)
	boundDigits := strconv.FormatInt(int64(supplychaincore.HolderKeyRangeMassGate)+1, 10)
	for _, lit := range []string{gateDigits, boundDigits} {
		if strings.Contains(string(body), lit) {
			t.Fatalf("mass source duplicates the %q literal: route the bound through supplychaincore.HolderKeyRangeMassGate", lit)
		}
	}
	if !strings.Contains(string(body), "HolderKeyRangeMassGate") {
		t.Fatalf("mass source no longer references supplychaincore.HolderKeyRangeMassGate")
	}
}
