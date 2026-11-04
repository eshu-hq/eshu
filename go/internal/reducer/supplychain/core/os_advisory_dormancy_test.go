// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestOSVReleaseTagsReachTheReducerAfterActivation is the prescribed inverse
// of the deleted dormancy pin TestOSVReleaseTagsStayDormantUntilTheSDKCarriesThem
// (#9806 PR-2b-1, ruling decision 7). The typed factschema now declares
// AffectedRange.Releases (since rc.13; go.mod pins rc.14) and the reducer decode
// carries it, so the bookworm row below reads affected_exact on its own line.
func TestOSVReleaseTagsReachTheReducerAfterActivation(t *testing.T) {
	t.Parallel()

	envelopes := convertedOSVFixture(t, "debian-cve-2023-38039.json", false)

	tagged := false
	decodedHostLine := false
	for _, envelope := range envelopes {
		if envelope.FactKind != chain.VulnerabilityAffectedPackageFactKind {
			continue
		}
		ranges, _ := envelope.Payload["affected_ranges"].([]any)
		for _, item := range ranges {
			if rangeMap, _ := item.(map[string]any); rangeMap["releases"] != nil {
				tagged = true
			}
		}
		pkg, err := supplyChainAffectedPackageFromEnvelope(envelope)
		if err != nil {
			t.Fatalf("decode through the contracts seam: %v", err)
		}
		for _, affectedRange := range pkg.AffectedRanges {
			// The tag reaches the reducer: every converted OS range keeps
			// its releases through the typed decode. Each range carries
			// its own lines (bookworm debian:12, trixie debian:13, forky
			// debian:14); the bookworm tag is the host line the finding
			// below matches.
			if len(affectedRange.Releases) == 0 {
				t.Fatalf("decoded range lost its releases tag: %#v", affectedRange)
			}
			if slices.Contains(affectedRange.Releases, "debian:12") {
				decodedHostLine = true
			}
		}
	}
	if !tagged {
		t.Fatalf("converter wrote no releases key; the activation proves nothing")
	}
	if !decodedHostLine {
		t.Fatalf("no decoded range carries the bookworm host line debian:12")
	}

	findings := BuildSupplyChainImpactFindings(append(envelopes, debianCurl("12", "7.88.1-10+deb12u2").envelope()))
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
	}
	// Listed host line (ruling: only the host line's ranges and fixes decide,
	// and only this case can read affected_exact): bookworm Debian:12 is
	// listed, its range [0, 7.88.1-10+deb12u3) contains the observed version
	// with no fix at or below it, so the row reads affected_exact via the
	// release-aware exact path, never os_release_unverified.
	assertSupplyChainImpactStatus(t, findings[0], SupplyChainImpactAffectedExact)
	if findings[0].MatchReason != supplyChainVersionReasonDPKGAffectedRange {
		t.Fatalf("MatchReason = %q, want %q (the host line's own range)", findings[0].MatchReason, supplyChainVersionReasonDPKGAffectedRange)
	}
}
