// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"testing"
)

// TestScopedWritePublishesStampedFindingsOnly pins the #9779 writer rule: a
// scoped write upserts findings for stamped CVEs and drops everything else
// before the transaction, so rows outside the stamp are never republished.
func TestScopedWritePublishesStampedFindingsOnly(t *testing.T) {
	t.Parallel()

	write := SupplyChainImpactWrite{
		ScopeID:      "scope:s",
		GenerationID: "gen",
		ScopedCVEIDs: []string{"CVE-2026-9001"},
		Findings: []SupplyChainImpactFinding{
			{CVEID: "CVE-2026-9001", PackageID: "pkg-a"},
			{CVEID: "CVE-2026-9002", PackageID: "pkg-b"},
			{CVEID: "", PackageID: "pkg-c"},
		},
		FencingToken: 7,
	}
	got := filterSupplyChainImpactFindingsForScope(write)
	want := []SupplyChainImpactFinding{{CVEID: "CVE-2026-9001", PackageID: "pkg-a"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped findings = %+v, want %+v", got, want)
	}
	full := write
	full.ScopedCVEIDs = nil
	if got := filterSupplyChainImpactFindingsForScope(full); !reflect.DeepEqual(got, write.Findings) {
		t.Fatalf("full findings = %+v, want everything", got)
	}
}

// TestDerivedCVEIDsCoversWritesAndRetractions pins the #9740 disjoint input:
// every written row plus every row the retraction will tombstone, so the
// fan-out never mistakes a touched copy for untouched.
func TestDerivedCVEIDsCoversWritesAndRetractions(t *testing.T) {
	t.Parallel()

	rows := []changedRow{
		{factID: "new-a", stableKey: "k-a", digest: "d1", cveID: "CVE-2026-9001"},
		{factID: "same-b", stableKey: "k-b", digest: "d-same", cveID: "CVE-2026-9002"},
	}
	stored := []storedFinding{
		{factID: "same-b", generationID: "gen", stableKey: "k-b", digest: "d-same", cveID: "CVE-2026-9002"},
		{factID: "gone-c", generationID: "gen", stableKey: "k-c", digest: "d-old", cveID: "CVE-2026-9003"},
		{factID: "fresh-c", generationID: "gen", stableKey: "k-d", digest: "d-new", cveID: "CVE-2026-9004", fencingToken: 9},
	}
	got := derivedCVEIDs(rows, stored, "gen", 7, true, nil)
	want := []string{"CVE-2026-9001", "CVE-2026-9002", "CVE-2026-9003"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("derived = %v, want %v (written plus retracted, fresher-token row excluded)", got, want)
	}
	if got := derivedCVEIDs(rows, stored, "gen", 7, false, nil); !reflect.DeepEqual(got, []string{"CVE-2026-9001", "CVE-2026-9002"}) {
		t.Fatalf("derived without retract = %v, want written only", got)
	}
	// A scoped pass's meta is already stamp-filtered by the writer; the
	// scope below restricts the retracted side only: the out-of-stamp
	// stored row is never tombstoned and must not be reported.
	scopedRows := []changedRow{
		{factID: "new-a", stableKey: "k-a", digest: "d1", cveID: "CVE-2026-9001"},
	}
	scopedStored := []storedFinding{
		{factID: "other", generationID: "gen", stableKey: "k-x", digest: "dx", cveID: "CVE-2026-9001"},
		{factID: "outside", generationID: "gen", stableKey: "k-y", digest: "dy", cveID: "CVE-2026-9002"},
	}
	if got := derivedCVEIDs(scopedRows, scopedStored, "gen", 7, true, []string{"CVE-2026-9001"}); !reflect.DeepEqual(got, []string{"CVE-2026-9001"}) {
		t.Fatalf("scoped derived = %v, want the stamp only: rows outside it are never tombstoned", got)
	}
}
