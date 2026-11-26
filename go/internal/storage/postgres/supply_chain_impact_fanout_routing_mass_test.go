// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestHolderRoutingMassQueryBoundsMemberArm pins the #10972 H2 Lane-1b
// routing-count shape: the same three per-id LATERAL arms as the probe, but
// the direct (cve_id, advisory_id) arms stay exact-to-gate (LIMIT gate+1,
// saturate at gate+1) while the group-members arm is bound at the probe
// fill (LIMIT 101, saturate to gate+1 above 100 member rows). The routing
// truth table the evidence note states: ranged iff direct capped sum >
// gate, OR member rows > 100, OR exact sum > gate with member <= 100.
// The gate-sized member fetch of the as-built count is the measured defect
// (coordinator: warm timeouts, then multi-second warms on member-carrying
// sets); the micro-shim proves this shape warm p95/max under the bars with
// zero timeouts. It fails until the member-bound amendment lands.
func TestHolderRoutingMassQueryBoundsMemberArm(t *testing.T) {
	t.Parallel()

	q := HolderRoutingMassQuery
	gate := int64(supplychaincore.HolderKeyRangeMassGate)
	bound := gate + 1
	boundDigits := strconv.FormatInt(bound, 10)

	if strings.Contains(q, ";") {
		t.Fatalf("routing-mass count holds more than one statement:\n%s", q)
	}
	for _, want := range []string{"LATERAL", "LIMIT", "LEAST"} {
		if !strings.Contains(q, want) {
			t.Fatalf("routing-mass count lost %q:\n%s", want, q)
		}
	}
	if strings.Contains(q, "UNION") {
		t.Fatalf("routing-mass count aggregates arms with UNION instead of per-id LATERALs:\n%s", q)
	}
	if strings.Contains(q, "DISTINCT") {
		t.Fatalf("routing-mass count deduplicates matches with DISTINCT, flipping the overestimate bias:\n%s", q)
	}
	if strings.Contains(q, "EXISTS") {
		t.Fatalf("routing-mass count uses the rejected EXISTS-to-cap shape:\n%s", q)
	}
	if got := strings.Count(q, "LEFT JOIN"); got != 3 {
		t.Fatalf("routing-mass count joins %d times, want 3 (one per-id LATERAL arm) with zero-fill", got)
	}
	if got := strings.Count(q, "FROM supply_chain_impact_finding_supports AS support"); got != 3 {
		t.Fatalf("routing-mass count reads the supports table %d times, want 3 (one per arm)", got)
	}
	if got := strings.Count(q, "<> ''"); got != 2 {
		t.Fatalf("routing-mass count carries the non-empty predicate %d times, want 2 (the cve_id and advisory_id arms prove their partial indexes)", got)
	}
	if !strings.Contains(q, "advisory_group_members") {
		t.Fatalf("routing-mass count lost the group-members arm")
	}
	// The direct arms stay exact-to-gate: saturate at the bound above the
	// gate over a gate+1 fetch.
	directVerb := "CASE WHEN COUNT(*) > " + boundDigits +
		" THEN " + boundDigits + " ELSE COUNT(*) END"
	if got := strings.Count(q, directVerb); got != 2 {
		t.Fatalf("routing-mass count carries %d gate-bound direct verbs, want 2 (cve_id and advisory_id arms):\n%s", got, q)
	}
	if got := strings.Count(q, "LIMIT "+strconv.FormatInt(bound+1, 10)); got != 2 {
		t.Fatalf("routing-mass count carries %d gate-sized fetches, want 2 (the direct arms):\n%s", got, q)
	}
	// The member arm is bound at the probe fill and saturates high: a
	// member mass over HolderMassProbeFillToCapRows reports gate+1, so the
	// member blind band can only over-route, never under-route.
	memberVerb := "CASE WHEN COUNT(*) > " +
		strconv.FormatInt(int64(supplychaincore.HolderMassProbeFillToCapRows), 10) +
		" THEN " + boundDigits + " ELSE COUNT(*) END"
	if got := strings.Count(q, memberVerb); got != 1 {
		t.Fatalf("routing-mass count carries %d member-bound verbs, want 1 (the group-members arm):\n%s", got, q)
	}
	if got := strings.Count(q, "LIMIT "+strconv.FormatInt(int64(supplychaincore.HolderMassProbeFillToCapRows)+1, 10)); got != 1 {
		t.Fatalf("routing-mass count carries %d probe-cost member fetches, want 1 (LIMIT 101):\n%s", got, q)
	}
	// The bound reads 7 times: the LEAST total, two direct comparisons,
	// two direct CASE saturation values, the member CASE saturation value,
	// and the already-hot short-circuit.
	if got := strings.Count(q, boundDigits); got != 7 {
		t.Fatalf("routing-mass count carries the bound %d times, want 7 (LEAST total, two direct comparisons, two direct saturation values, one member saturation value, already-hot short-circuit)", got)
	}
	for _, banned := range []string{"fact_records", "is_tombstone"} {
		if strings.Contains(q, banned) {
			t.Fatalf("routing-mass count still reads the finding facts (%q)", q)
		}
	}
}

// TestHolderRoutingMassBoundIsSingleSourced fails a duplicated bound
// literal: the template carries 11 %d verbs (LEAST total, three per-arm
// threshold/saturation/LIMIT triples, already-hot short-circuit), and every
// bound flows from supplychaincore.HolderKeyRangeMassGate (direct arms) or
// supplychaincore.HolderMassProbeFillToCapRows (member arm).
func TestHolderRoutingMassBoundIsSingleSourced(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("supply_chain_impact_fanout_mass.go")
	if err != nil {
		t.Fatalf("read the mass source: %v", err)
	}
	for _, lit := range []string{"78435", "78436", "78437"} {
		if strings.Contains(string(body), lit) {
			t.Fatalf("mass source duplicates the %q literal: route the bound through supplychaincore.HolderKeyRangeMassGate", lit)
		}
	}
	if !strings.Contains(string(body), "HolderKeyRangeMassGate") {
		t.Fatalf("mass source no longer references supplychaincore.HolderKeyRangeMassGate")
	}
	if !strings.Contains(string(body), "HolderMassProbeFillToCapRows") {
		t.Fatalf("mass source no longer references supplychaincore.HolderMassProbeFillToCapRows for the member bound")
	}
	marker := []byte("const holderMassProbeQueryTemplate = `")
	start := bytes.Index(body, marker)
	if start < 0 {
		t.Fatalf("mass source lost the holder-mass template const")
	}
	template := string(body[start+len(marker):])
	if end := strings.Index(template, "`"); end > 0 {
		template = template[:end]
	}
	if got := strings.Count(template, "%d"); got != 11 {
		t.Fatalf("holder-mass template carries %d %%d verbs, want 11 (LEAST total, three per-arm triples, already-hot short-circuit): a hardcoded literal bypasses the single sources", got)
	}
}

// TestHolderMassProbeQueryIsByteIdentical pins the Lane-1b probe invariant:
// the amendment must not move the shipped probe text a byte. The golden is
// the exact pre-amendment builder output (rendered by the temporary Lane-1b
// generator, never hand-copied); regenerate it the same way after any
// deliberate probe change and review the diff.
func TestHolderMassProbeQueryIsByteIdentical(t *testing.T) {
	t.Parallel()

	golden, err := os.ReadFile("testdata/holder_mass_probe_golden.sql")
	if err != nil {
		t.Fatalf("read the probe golden: %v", err)
	}
	if HolderMassProbeQuery != string(golden) {
		t.Fatalf("probe text drifted from the golden by %d bytes (probe %d, golden %d): the probe is byte-pinned, change it only deliberately with a regenerated golden",
			len(HolderMassProbeQuery)-len(golden), len(HolderMassProbeQuery), len(golden))
	}
}
