// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "testing"

// TestComparatorRangeReadsNPMHyphenRange pins the #9865 latent defect:
// comparatorBranchContains returned "valid, not contained" on the first
// unmatched field and never read the rest, so the npm hyphen range
// "2.0.0 - 2.3.0" read as "=2.0.0".
func TestComparatorRangeReadsNPMHyphenRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		raw          string
		observed     string
		wantContains bool
		wantValid    bool
	}{
		{"inside", "2.0.0 - 2.3.0", "2.2.0", true, true},
		{"lower bound is inclusive", "2.0.0 - 2.3.0", "2.0.0", true, true},
		{"upper bound is inclusive", "2.0.0 - 2.3.0", "2.3.0", true, true},
		{"below", "2.0.0 - 2.3.0", "1.9.9", false, true},
		{"above", "2.0.0 - 2.3.0", "2.3.1", false, true},
		{"second branch of an or", "1.0.0 - 1.2.0 || 2.0.0 - 2.3.0", "2.2.0", true, true},
		{"first branch of an or", "1.0.0 - 1.2.0 || 2.0.0 - 2.3.0", "1.1.0", true, true},
		{"outside both branches", "1.0.0 - 1.2.0 || 2.0.0 - 2.3.0", "1.5.0", false, true},
		{"hyphen beside a comparator", "2.0.0 - 2.3.0 !=2.2.0", "2.2.0", false, true},
		{"prerelease bound", "2.0.0-rc.1 - 2.3.0", "2.0.0", true, true},
		// A partial bound means a range npm widens (1.2 - 2.3 is >=1.2.0 <2.4.0);
		// the matcher cannot expand it, so it fails closed.
		{"partial upper bound fails closed", "1.2.0 - 2.3", "2.3.5", false, false},
		{"partial lower bound fails closed", "1.2 - 2.3.0", "1.2.5", false, false},
		{"dangling hyphen", "2.0.0 -", "2.2.0", false, false},
		{"leading hyphen", "- 2.3.0", "2.2.0", false, false},
		{"double hyphen", "2.0.0 - 2.3.0 - 2.4.0", "2.2.0", false, false},
		{"hyphen with a non-version bound", "2.0.0 - latest", "2.2.0", false, false},
		{"hyphen with an operator bound", ">=2.0.0 - 2.3.0", "2.2.0", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, valid := comparatorRangeContains(tc.raw, tc.observed, compareOSVSemver)
			if got != tc.wantContains || valid != tc.wantValid {
				t.Fatalf("comparatorRangeContains(%q, %q) = (%v, %v), want (%v, %v)",
					tc.raw, tc.observed, got, valid, tc.wantContains, tc.wantValid)
			}
		})
	}
}

// TestComparatorBranchValidatesEveryField pins that a branch is not read as
// "valid, not contained" once one field fails: a later malformed field makes
// the whole branch malformed.
func TestComparatorBranchValidatesEveryField(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		raw          string
		observed     string
		wantContains bool
		wantValid    bool
	}{
		{"unmatched field then a malformed one", ">=2.0.0 <garbage", "1.0.0", false, false},
		{"unmatched equality then a malformed one", "2.0.0 garbage", "1.0.0", false, false},
		{"all fields readable and unmatched", ">=2.0.0 <3.0.0", "1.0.0", false, true},
		{"all fields readable and matched", ">=1.0.0 <3.0.0", "2.0.0", true, true},
		{"matched first field, malformed second", ">=1.0.0 <garbage", "2.0.0", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, valid := comparatorRangeContains(tc.raw, tc.observed, compareOSVSemver)
			if got != tc.wantContains || valid != tc.wantValid {
				t.Fatalf("comparatorRangeContains(%q, %q) = (%v, %v), want (%v, %v)",
					tc.raw, tc.observed, got, valid, tc.wantContains, tc.wantValid)
			}
		})
	}
}

// TestNPMHyphenRawRangeReachesTheVerdict pins the end-to-end effect: an npm
// advisory whose only range is a raw hyphen range must be affected inside it,
// and must not read known-fixed from a scalar fix below the observed version.
func TestNPMHyphenRawRangeReachesTheVerdict(t *testing.T) {
	t.Parallel()
	pkgs := affectedPackageWithRanges()
	pkgs[0].AffectedRangeRaw = "2.0.0 - 2.3.0"
	if got := evaluateSupplyChainVersionMatch("npm", "2.2.0", "", "", pkgs); string(got.Status) != "affected_exact" {
		t.Fatalf("blank scalar: status=%q reason=%q, want affected_exact", got.Status, got.Reason)
	}
	// The scalar path: a scalar fix of 2.0.0 sits below 2.2.0, which the raw
	// range still affects. Containment runs first, so affected wins.
	if got := evaluateSupplyChainVersionMatch("npm", "2.2.0", "", "2.0.0", pkgs); string(got.Status) != "affected_exact" {
		t.Fatalf("scalar below observed: status=%q reason=%q, want affected_exact", got.Status, got.Reason)
	}
	if got := evaluateSupplyChainVersionMatch("npm", "2.3.1", "", "", pkgs); string(got.Status) != "possibly_affected" {
		t.Fatalf("above the range: status=%q reason=%q, want possibly_affected", got.Status, got.Reason)
	}
}
