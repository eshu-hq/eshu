// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// rpm joins the shared OS decision only when some fact carries a typed
// ECOSYSTEM range (rpm_version.go), so the rpm rows live apart from the
// dpkg/apk table: that table's scalar-fix-only OSV facts take the legacy rpm
// loop instead.
const (
	rpmHost       = "7.61.1-23.el8"
	rpmFixBelow   = "7.61.1-22.el8"
	rpmOtherHost  = "7.61.1-24.el8"
	rpmUnreadable = "-"
)

func rpmOSVFact() supplychainmodel.AffectedPackage {
	return supplychainmodel.AffectedPackage{
		CVEID: "CVE-2099-0929", Source: "osv", AdvisoryID: "ALSA-2099-0929",
		PackageID: "os://rpm/curl", Ecosystem: "os", Name: "curl",
		PURL:           "pkg:rpm/alma/curl",
		AffectedRanges: []supplychainmodel.AffectedRange{osIntroducedFixedRange("0", rpmFixBelow)},
		FixedVersions:  []string{rpmFixBelow},
	}
}

func rpmVendorFact(versions ...string) supplychainmodel.AffectedPackage {
	return supplychainmodel.AffectedPackage{
		CVEID: "CVE-2099-0929", Source: "alma", AdvisoryID: "ALSA-2099-0929-V",
		PackageID: "os://alma/curl", Ecosystem: "os", Name: "curl",
		PURL: "pkg:rpm/alma/curl", AffectedVersions: versions,
		// The shape the join hands the matcher once a line is verified. No rpm
		// row exists in the host-line table, so the join cannot verify one
		// today (see TestRPMVendorFactIsUnverifiedUntilTheHostLineIsReadable).
		ReleaseLine: "alma:8",
	}
}

// TestRPMVendorFactIsUnverifiedUntilTheHostLineIsReadable pins what the join
// does with an rpm vendor fact today: osHostReleaseLine has no rpm row, so the
// host line is unknown and the fact reads possibly_affected, never exact,
// until an rpm producer and line reader are designed together.
func TestRPMVendorFactIsUnverifiedUntilTheHostLineIsReadable(t *testing.T) {
	t.Parallel()
	row := supplychainmodel.OSPackage{Distro: "alma", DistroVersion: "8.9", VendorAdvisorySource: "alma"}
	vendor := rpmVendorFact(rpmHost)
	vendor.ReleaseLine = ""
	pkgs := selectOSAdvisoryRelease([]supplychainmodel.AffectedPackage{rpmOSVFact(), vendor}, row)
	got := evaluateSupplyChainVersionMatch("os", rpmHost, "", rpmFixBelow, pkgs)
	// The vendor list names the host with no release evidence while the
	// OSV range says it is fixed: the host stays possibly affected.
	assertOSDecision(t, got, SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseAmbiguous)
}

// TestVendorVersionListBeatsOSVRangesRPM pins the rpm path of #9929: an
// untagged OSV rpm fact whose range fixes the version below the host reads
// known fixed alone, and a vendor fact whose exact list names the host turns
// that into affected_exact under the existing rpm reasons, in either fact
// order. An unreadable list entry never counts as the host and keeps the
// known-fixed answer from standing.
func TestVendorVersionListBeatsOSVRangesRPM(t *testing.T) {
	t.Parallel()
	osv := rpmOSVFact()
	alone := evaluateSupplyChainVersionMatch("os", rpmHost, "", rpmFixBelow, []supplychainmodel.AffectedPackage{osv})
	assertOSDecision(t, alone, SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonRPMKnownFixed)

	for _, tc := range []struct {
		name   string
		vendor supplychainmodel.AffectedPackage
		status SupplyChainImpactStatus
		reason string
	}{
		{"list names the host", rpmVendorFact(rpmHost), SupplyChainImpactAffectedExact, supplyChainVersionReasonRPMExactAffected},
		{"host sits among other versions", rpmVendorFact(rpmOtherHost, rpmHost), SupplyChainImpactAffectedExact, supplyChainVersionReasonRPMExactAffected},
		{"list names another version", rpmVendorFact(rpmOtherHost), SupplyChainImpactNotAffectedKnownFixed, supplyChainVersionReasonRPMKnownFixed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bothOrders(t, []supplychainmodel.AffectedPackage{osv, tc.vendor}, func(t *testing.T, ordered []supplychainmodel.AffectedPackage) {
				got := evaluateSupplyChainVersionMatch("os", rpmHost, "", rpmFixBelow, ordered)
				assertOSDecision(t, got, tc.status, tc.reason)
			})
		})
	}

	t.Run("unreadable entry", func(t *testing.T) {
		t.Parallel()
		vendor := rpmVendorFact(rpmUnreadable)
		bothOrders(t, []supplychainmodel.AffectedPackage{osv, vendor}, func(t *testing.T, ordered []supplychainmodel.AffectedPackage) {
			got := evaluateSupplyChainVersionMatch("os", rpmHost, "", rpmFixBelow, ordered)
			assertOSDecision(t, got, SupplyChainImpactPossiblyAffected, "malformed_advisory_range")
		})
	})
}
