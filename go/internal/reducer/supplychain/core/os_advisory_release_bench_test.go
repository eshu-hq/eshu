// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// BenchmarkSelectOSAdvisoryRelease measures the per-finding narrowing step
// (#9806 PR-2b) on one tagged OSV fact: a listed host line (copy and narrow),
// an unlisted one (copy and mark), an untagged fact (early return), and a
// host whose release maps to no line (early return before any fact), plus the
// untagged fact with a vendor-sourced installed row and the three outcomes of
// the vendor line check (#9945).
func BenchmarkSelectOSAdvisoryRelease(b *testing.B) {
	untagged := osvDebianCurlFact()
	untagged.AffectedRanges = []supplychainmodel.AffectedRange{
		{Kind: "ECOSYSTEM", Events: untagged.AffectedRanges[0].Events},
		{Kind: "ECOSYSTEM", Events: untagged.AffectedRanges[1].Events},
	}
	for _, tc := range []struct {
		name string
		pkg  supplychainmodel.AffectedPackage
		row  supplychainmodel.OSPackage
	}{
		{"listed", osvDebianCurlFact(), supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "12"}},
		{"unlisted", osvDebianCurlFact(), supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "11"}},
		{"untagged", untagged, supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "12"}},
		{"host-unknown", osvDebianCurlFact(), supplychainmodel.OSPackage{Distro: "debian"}},
		// The vendor line check (#9945) adds one source compare per fact that
		// is neither tagged nor selectable. A real installed row always has a
		// vendor advisory source, so the untagged row repeats with one.
		{"untagged-vendor-row", untagged, bookwormRow},
		{"vendor-matched", vendorLineFact("debian", "pkg:deb/debian/curl?arch=amd64&distro=debian-12"), bookwormRow},
		{"vendor-mismatch", vendorLineFact("debian", "pkg:deb/debian/curl?arch=amd64&distro=debian-11"), bookwormRow},
		{"vendor-unknown", vendorLineFact("debian", "pkg:deb/debian/curl?arch=amd64"), bookwormRow},
	} {
		b.Run(tc.name, func(b *testing.B) {
			pkgs := []supplychainmodel.AffectedPackage{tc.pkg}
			b.ReportAllocs()
			var sink []supplychainmodel.AffectedPackage
			for i := 0; i < b.N; i++ {
				sink = selectOSAdvisoryRelease(pkgs, tc.row)
			}
			_ = sink
		})
	}
}
