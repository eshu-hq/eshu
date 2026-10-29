// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestSameAuthorizedAdvisoryPackageMatchesEachObservationAgainstFinding pins
// the conflict-gate identity: two authorized observations belong to one
// advisory identity only when each of them independently matches the
// finding's (CVE, advisory, package) triple. A pair that agrees with each
// other but not with the finding must not conflict, and neither must a pair
// where only one side matches the finding.
func TestSameAuthorizedAdvisoryPackageMatchesEachObservationAgainstFinding(t *testing.T) {
	t.Parallel()
	observation := func(cve, advisory, pkg string) supplychainmodel.AffectedPackage {
		return supplychainmodel.AffectedPackage{CVEID: cve, AdvisoryID: advisory, PackageID: pkg}
	}
	finding := SupplyChainImpactFinding{CVEID: "CVE-2026-9299", AdvisoryID: "DSA-2026-1", PackageID: "pkg:deb/debian/openssl"}
	for _, tc := range []struct {
		name        string
		left, right supplychainmodel.AffectedPackage
		want        bool
	}{
		{
			name:  "both observations match finding",
			left:  observation("CVE-2026-9299", "DSA-2026-1", "pkg:deb/debian/openssl"),
			right: observation("CVE-2026-9299", "DSA-2026-1", "pkg:deb/debian/openssl"),
			want:  true,
		},
		{
			name:  "pair agrees but finding CVE differs",
			left:  observation("CVE-2026-0001", "DSA-2026-1", "pkg:deb/debian/openssl"),
			right: observation("CVE-2026-0001", "DSA-2026-1", "pkg:deb/debian/openssl"),
			want:  false,
		},
		{
			name:  "only left matches finding CVE",
			left:  observation("CVE-2026-9299", "DSA-2026-1", "pkg:deb/debian/openssl"),
			right: observation("CVE-2026-0001", "DSA-2026-1", "pkg:deb/debian/openssl"),
			want:  false,
		},
		{
			name:  "only right matches finding CVE",
			left:  observation("CVE-2026-0001", "DSA-2026-1", "pkg:deb/debian/openssl"),
			right: observation("CVE-2026-9299", "DSA-2026-1", "pkg:deb/debian/openssl"),
			want:  false,
		},
		{
			name:  "right advisory differs",
			left:  observation("CVE-2026-9299", "DSA-2026-1", "pkg:deb/debian/openssl"),
			right: observation("CVE-2026-9299", "DSA-2026-2", "pkg:deb/debian/openssl"),
			want:  false,
		},
		{
			name:  "finding package differs from agreeing pair",
			left:  observation("CVE-2026-9299", "DSA-2026-1", "pkg:deb/debian/zlib"),
			right: observation("CVE-2026-9299", "DSA-2026-1", "pkg:deb/debian/zlib"),
			want:  false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sameAuthorizedAdvisoryPackage(tc.left, tc.right, finding); got != tc.want {
				t.Fatalf("sameAuthorizedAdvisoryPackage = %t, want %t", got, tc.want)
			}
		})
	}
}
