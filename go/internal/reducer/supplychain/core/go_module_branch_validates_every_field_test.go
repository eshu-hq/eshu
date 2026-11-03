// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "testing"

// TestGoModuleComparatorBranchValidatesEveryField pins the Go sibling of
// TestComparatorBranchValidatesEveryField (#9865): an AND branch must read
// every field before it answers. A field that does not match must not hide a
// later malformed one, or the branch reads as "valid, not contained" instead
// of malformed. Both verdicts are non-affected, but the reason code differs
// and a malformed range must be reported as malformed.
func TestGoModuleComparatorBranchValidatesEveryField(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		branch       string
		observed     string
		wantContains bool
		wantValid    bool
	}{
		{"unmatched field then a malformed one", ">=2.0.0 <garbage", "1.0.0", false, false},
		{"unmatched equality then a malformed one", "2.0.0 garbage", "1.0.0", false, false},
		{"matched first field, malformed second", ">=1.0.0 <garbage", "2.0.0", false, false},
		{"all fields readable and unmatched", ">=2.0.0 <3.0.0", "1.0.0", false, true},
		{"all fields readable and matched", ">=1.0.0 <3.0.0", "2.0.0", true, true},
		{"zero floor with a malformed second field", ">=0.0.0 <garbage", "1.0.0", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, valid := goModuleComparatorBranchContains(tc.branch, tc.observed)
			if got != tc.wantContains || valid != tc.wantValid {
				t.Fatalf("goModuleComparatorBranchContains(%q, %q) = (%v, %v), want (%v, %v)",
					tc.branch, tc.observed, got, valid, tc.wantContains, tc.wantValid)
			}
		})
	}
}

// TestGoModuleComparatorRangeKeepsAMatchingBranchBesideAMalformedOne pins that
// reading every field of a branch does not let a malformed branch hide a
// containing one in an OR range.
func TestGoModuleComparatorRangeKeepsAMatchingBranchBesideAMalformedOne(t *testing.T) {
	t.Parallel()
	got, valid := goModuleComparatorRangeContains(">=2.0.0 <garbage || >=1.0.0 <3.0.0", "1.5.0")
	if !got || !valid {
		t.Fatalf("goModuleComparatorRangeContains = (%v, %v), want (true, true)", got, valid)
	}
}
