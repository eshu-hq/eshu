// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestDetectionProfileIsAPerSupportOutputOfOneCanonicalKey records, from the
// writer, that two findings with the same canonical-key inputs can differ in
// precise eligibility (#10880 PR-B0 profile proof).
//
// The canonical key names the advisory, package, observed version, requested
// range, impact status, repository, and subject digest. It does not name the
// match reason, and the package path never sets the product criteria or match
// criteria id. classifySupplyChainImpactDetectionProfile reads status, observed
// version, and match reason. So two supports of one key, written from scopes
// whose evidence differs, share status and observed version and can still
// carry different match reasons. On the OS path both outcomes below are
// affected_exact for the same observed version and release line: one scope
// holds a vendor fact whose exact version list names the host
// (dpkg_exact_affected_version, precise); another holds only a tagged OSV range
// that contains it (dpkg_affected_range, comprehensive).
func TestDetectionProfileIsAPerSupportOutputOfOneCanonicalKey(t *testing.T) {
	t.Parallel()
	const (
		observed = "1.0-1"
		line     = "ubuntu:22.04"
	)
	vendorList := []supplychainmodel.AffectedPackage{{
		Source: "ubuntu", AdvisoryID: "USN-1", Ecosystem: "ubuntu",
		AffectedVersions: []string{observed}, ReleaseLine: line,
	}}
	osvRange := []supplychainmodel.AffectedPackage{{
		Source: "osv", AdvisoryID: "OSV-1", Ecosystem: "ubuntu", ReleaseLine: line,
		AffectedRanges: []supplychainmodel.AffectedRange{{
			Kind:     "ECOSYSTEM",
			Releases: []string{line},
			Events: []supplychainmodel.AffectedRangeEvent{
				{Introduced: "0"},
				{Fixed: "2.0-1"},
			},
		}},
	}}

	finding := func(pkgs []supplychainmodel.AffectedPackage) SupplyChainImpactFinding {
		decision := evaluateSupplyChainVersionMatch("ubuntu", observed, "", "", pkgs)
		out := SupplyChainImpactFinding{
			Status: decision.Status, ObservedVersion: observed, MatchReason: decision.Reason,
		}
		out.DetectionProfile = classifySupplyChainImpactDetectionProfile(out)
		return out
	}
	fromList, fromRange := finding(vendorList), finding(osvRange)

	if fromList.Status != SupplyChainImpactAffectedExact || fromRange.Status != SupplyChainImpactAffectedExact {
		t.Fatalf("status = %s / %s, want affected_exact for both (the key's impact_status is equal)",
			fromList.Status, fromRange.Status)
	}
	if fromList.MatchReason != supplyChainVersionReasonDPKGExactAffected ||
		fromRange.MatchReason != supplyChainVersionReasonDPKGAffectedRange {
		t.Fatalf("match reasons = %q / %q, want %q / %q",
			fromList.MatchReason, fromRange.MatchReason,
			supplyChainVersionReasonDPKGExactAffected, supplyChainVersionReasonDPKGAffectedRange)
	}
	if fromList.DetectionProfile != DetectionProfilePrecise || fromRange.DetectionProfile != DetectionProfileComprehensive {
		t.Fatalf("profiles = %s / %s, want precise / comprehensive: precise eligibility is not a function of the canonical key",
			fromList.DetectionProfile, fromRange.DetectionProfile)
	}
}
