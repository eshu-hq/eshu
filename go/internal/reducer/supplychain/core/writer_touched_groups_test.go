// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"testing"
)

// The #10411 touched-group set: coalesced (cve_id if non-blank, else
// advisory_id, trimmed) over the rows a pass changed, including the
// stored rows it retracts and the prior side of a row it rewrites under
// a different group. ChangedCVEIDs and the fan-out are untouched by it;
// these tests pin only the additive set.
func TestTouchedGroupIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rows    []changedRow
		stored  []storedFinding
		retract bool
		want    []string
	}{
		{
			name: "grouped row keys on its CVE",
			rows: []changedRow{
				{factID: "a", stableKey: "k-a", digest: "d1", cveID: "CVE-2026-0001", advisoryID: "GHSA-a"},
			},
			want: []string{"CVE-2026-0001"},
		},
		{
			name: "CVE-less row keys on its advisory id",
			rows: []changedRow{
				{factID: "a", stableKey: "k-a", digest: "d1", cveID: " ", advisoryID: " DEBIAN-CVE-2026-0001 "},
			},
			want: []string{"DEBIAN-CVE-2026-0001"},
		},
		{
			name: "row with neither id names no group",
			rows: []changedRow{
				{factID: "a", stableKey: "k-a", digest: "d1"},
			},
			want: nil,
		},
		{
			name: "unchanged rewrite touches nothing",
			rows: []changedRow{
				{factID: "same", stableKey: "k-a", digest: "d-same", cveID: "CVE-2026-0001", advisoryID: "GHSA-a"},
			},
			stored: []storedFinding{
				{factID: "same", generationID: "gen", stableKey: "k-a", digest: "d-same", cveID: "CVE-2026-0001", advisoryID: "GHSA-a"},
			},
			want: nil,
		},
		{
			name: "changed row touches the old group as well as the new",
			rows: []changedRow{
				{factID: "same", stableKey: "k-a", digest: "d-new", cveID: "CVE-2026-0002", advisoryID: "GHSA-b"},
			},
			stored: []storedFinding{
				{factID: "same", generationID: "gen", stableKey: "k-a", digest: "d-old", cveID: "CVE-2026-0001", advisoryID: "GHSA-a"},
			},
			want: []string{"CVE-2026-0001", "CVE-2026-0002"},
		},
		{
			name: "retracted stored row touches its advisory-keyed group",
			rows: []changedRow{
				{factID: "kept", stableKey: "k-a", digest: "d1", cveID: "CVE-2026-0001"},
			},
			stored: []storedFinding{
				{factID: "kept", generationID: "gen", stableKey: "k-a", digest: "d0", cveID: "CVE-2026-0001"},
				{factID: "gone", generationID: "gen", stableKey: "k-b", digest: "d2", advisoryID: "DSA-6000-1"},
			},
			retract: true,
			want:    []string{"CVE-2026-0001", "DSA-6000-1"},
		},
		{
			name: "partial pass retracts nothing and proves no removal",
			rows: []changedRow{
				{factID: "kept", stableKey: "k-a", digest: "d1", cveID: "CVE-2026-0001"},
			},
			stored: []storedFinding{
				{factID: "kept", generationID: "gen", stableKey: "k-a", digest: "d0", cveID: "CVE-2026-0001"},
				{factID: "gone", generationID: "gen", stableKey: "k-b", digest: "d2", advisoryID: "DSA-6000-1"},
			},
			retract: false,
			want:    []string{"CVE-2026-0001"},
		},
		{
			name: "first pass judges the previous generation by stable key",
			rows: []changedRow{
				{factID: "new", stableKey: "k-a", digest: "d1", advisoryID: "GHSA-new"},
			},
			stored: []storedFinding{
				{factID: "prior-a", generationID: "prior", stableKey: "k-a", digest: "d0", advisoryID: "GHSA-old"},
				{factID: "prior-b", generationID: "prior", stableKey: "k-b", digest: "d2", cveID: "CVE-2026-0009"},
			},
			retract: true,
			want:    []string{"CVE-2026-0009", "GHSA-new", "GHSA-old"},
		},
		{
			name: "row a fresher fencing token protects is not a retraction",
			rows: []changedRow{
				{factID: "kept", stableKey: "k-a", digest: "d1", cveID: "CVE-2026-0001"},
			},
			stored: []storedFinding{
				{factID: "kept", generationID: "gen", stableKey: "k-a", digest: "d0", cveID: "CVE-2026-0001"},
				{factID: "fresh", generationID: "gen", stableKey: "k-b", digest: "d2", cveID: "CVE-2026-0002", fencingToken: 9},
			},
			retract: true,
			want:    []string{"CVE-2026-0001"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := touchedGroupIDs(test.rows, test.stored, "gen", 7, test.retract)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("touchedGroupIDs = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCoalescedFindingGroupID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cveID      string
		advisoryID string
		want       string
	}{
		{cveID: "CVE-2026-0001", advisoryID: "GHSA-a", want: "CVE-2026-0001"},
		{cveID: " CVE-2026-0001 ", advisoryID: "GHSA-a", want: "CVE-2026-0001"},
		{cveID: "", advisoryID: "GHSA-a", want: "GHSA-a"},
		{cveID: "  ", advisoryID: " GHSA-a ", want: "GHSA-a"},
		{cveID: "", advisoryID: "", want: ""},
	}
	for _, test := range tests {
		if got := coalescedFindingGroupID(test.cveID, test.advisoryID); got != test.want {
			t.Fatalf("coalescedFindingGroupID(%q, %q) = %q, want %q", test.cveID, test.advisoryID, got, test.want)
		}
	}
}
