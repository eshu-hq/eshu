// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
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

// crossReleaseUbuntuEnvelopes builds two same-triple raw affected-package
// facts whose version-disjoint ECOSYSTEM ranges name different release
// lines: fix A rides ubuntu:22.04 over [7.68.0-1ubuntu2.18, ...2.20), fix B
// rides ubuntu:24.04 over [7.81.0-1ubuntu1.10, ...1.15). The disjoint windows
// prove different branches, so the conflict gate stays quiet and branch
// selection decides by observed-version containment.
func crossReleaseUbuntuEnvelopes() []facts.Envelope {
	affected := func(factID, introduced, fixed, listed, line string) facts.Envelope {
		return facts.Envelope{
			FactID:   factID,
			FactKind: chain.VulnerabilityAffectedPackageFactKind,
			Payload: map[string]any{
				"cve_id":            "UBUNTU-CVE-2025-99991",
				"advisory_id":       "UBUNTU-CVE-2025-99991",
				"source":            "osv",
				"package_id":        "os://ubuntu/curl",
				"ecosystem":         "ubuntu",
				"package_name":      "curl",
				"purl":              "pkg:deb/ubuntu/curl?arch=source&distro=jammy",
				"affected_versions": []any{listed},
				"affected_ranges": []any{map[string]any{
					"type":     "ECOSYSTEM",
					"events":   []any{map[string]any{"introduced": introduced}, map[string]any{"fixed": fixed}},
					"releases": []any{line},
				}},
				"fixed_versions": []any{fixed},
			},
		}
	}
	return []facts.Envelope{
		affected("affected-jammy", "7.68.0-1ubuntu2.18", "7.68.0-1ubuntu2.20", "7.68.0-1ubuntu2.19", "ubuntu:22.04"),
		affected("affected-noble", "7.81.0-1ubuntu1.10", "7.81.0-1ubuntu1.15", "7.81.0-1ubuntu1.12", "ubuntu:24.04"),
	}
}

// TestCrossReleaseBranchLeavesUnmatchedHostUntouched pins the #9806 P1: the
// conflict path re-decodes raw affected packages and picks a branch by
// observed-version containment with no release-tag check, so a primary-path
// os_release_not_in_advisory verdict on an unmatched host (here focal 20.04,
// on neither tagged line) was replaced by the foreign line's fix. The focal
// observed version sits inside fix B's window only, so containment alone
// selects noble's fix. The verdict must stand: possibly_affected /
// os_release_not_in_advisory, no foreign fixed version.
func TestCrossReleaseBranchLeavesUnmatchedHostUntouched(t *testing.T) {
	t.Parallel()

	rows := []supplychainmodel.OSPackage{
		{FactID: "os-row-focal", Distro: "ubuntu", DistroVersion: "20.04"},
	}
	finding := SupplyChainImpactFinding{
		CVEID:           "UBUNTU-CVE-2025-99991",
		AdvisoryID:      "UBUNTU-CVE-2025-99991",
		PackageID:       "os://ubuntu/curl",
		Ecosystem:       "ubuntu",
		ObservedVersion: "7.81.0-1ubuntu1.12",
		Status:          SupplyChainImpactPossiblyAffected,
		MatchReason:     supplyChainVersionReasonOSReleaseNotInAdvisory,
		MissingEvidence: []string{supplyChainMissingOSReleaseNotListed},
		EvidenceFactIDs: []string{"os-row-focal"},
		EvidencePath: []string{
			chain.VulnerabilityCVEFactKind,
			chain.VulnerabilityAffectedPackageFactKind,
			chain.VulnerabilityOSPackageFactKind,
		},
	}
	got := []SupplyChainImpactFinding{finding}
	markAuthorizedFixedVersionConflicts(got, crossReleaseUbuntuEnvelopes(), rows)
	if got[0].Status != SupplyChainImpactPossiblyAffected ||
		got[0].MatchReason != supplyChainVersionReasonOSReleaseNotInAdvisory {
		t.Fatalf("finding = %s/%s, want %s/%s: the unmatched host keeps its primary verdict",
			got[0].Status, got[0].MatchReason,
			SupplyChainImpactPossiblyAffected, supplyChainVersionReasonOSReleaseNotInAdvisory)
	}
	if got[0].FixedVersion != "" {
		t.Fatalf("FixedVersion = %q, want blank: no foreign line's fix may present as the host's own", got[0].FixedVersion)
	}
	if !reflect.DeepEqual(got[0].MissingEvidence, []string{supplyChainMissingOSReleaseNotListed}) {
		t.Fatalf("MissingEvidence = %v, want the single not-in-advisory evidence", got[0].MissingEvidence)
	}
}

// TestCrossReleaseBranchKeepsHostLineSelection pins the other half of the
// #9806 P1 fix: when the host line IS listed, only its branches contend. A
// jammy 22.04 host facing the same cross-release pair keeps the primary
// verdict path open to its own line's fix and never takes noble's.
func TestCrossReleaseBranchKeepsHostLineSelection(t *testing.T) {
	t.Parallel()

	rows := []supplychainmodel.OSPackage{
		{FactID: "os-row-jammy", Distro: "ubuntu", DistroVersion: "22.04"},
	}
	finding := SupplyChainImpactFinding{
		CVEID:           "UBUNTU-CVE-2025-99991",
		AdvisoryID:      "UBUNTU-CVE-2025-99991",
		PackageID:       "os://ubuntu/curl",
		Ecosystem:       "ubuntu",
		ObservedVersion: "7.81.0-1ubuntu1.10",
		Status:          SupplyChainImpactPossiblyAffected,
		MatchReason:     supplyChainVersionReasonOSReleaseNotInAdvisory,
		MissingEvidence: []string{supplyChainMissingOSReleaseNotListed},
		EvidenceFactIDs: []string{"os-row-jammy"},
		EvidencePath: []string{
			chain.VulnerabilityCVEFactKind,
			chain.VulnerabilityAffectedPackageFactKind,
			chain.VulnerabilityOSPackageFactKind,
		},
	}

	got := []SupplyChainImpactFinding{finding}
	markAuthorizedFixedVersionConflicts(got, crossReleaseUbuntuEnvelopes(), rows)
	if got[0].FixedVersion == "7.81.0-1ubuntu1.15" {
		t.Fatalf("FixedVersion = %q, want never noble's fix on a jammy host", got[0].FixedVersion)
	}
}
