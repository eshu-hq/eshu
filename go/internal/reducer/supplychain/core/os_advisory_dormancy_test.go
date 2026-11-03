// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestOSVReleaseTagsStayDormantUntilTheSDKCarriesThem pins the #9806 PR-2b-1
// no-behavior-change contract (ruling decision 7). The converter now writes a
// "releases" key on each OS range, but factschema v0.1.0-rc.12 has no field
// for it, so the real contracts seam (DecodeAffectedPackage) drops the key and
// the reducer never sees a tag. Every joined OSV OS fact therefore keeps its
// PR-2a verdict. The bookworm row below is the one PR-2b's activation changes:
// once the SDK carries Releases and the decode copies it, it reads
// affected_exact on its own line.
//
// The activation PR deletes this test; its inverse (the tag reaches the
// reducer and the same row reads affected_exact) becomes the end-to-end RED.
func TestOSVReleaseTagsStayDormantUntilTheSDKCarriesThem(t *testing.T) {
	t.Parallel()

	envelopes := convertedOSVFixture(t, "debian-cve-2023-38039.json", false)

	tagged := false
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
			t.Fatalf("decode through the contracts seam: %v (an extra releases key must stay tolerated)", err)
		}
		for _, affectedRange := range pkg.AffectedRanges {
			if affectedRange.Releases != nil {
				t.Fatalf("decoded range carries Releases %v; the tag is dormant until the SDK pin that declares it", affectedRange.Releases)
			}
		}
	}
	if !tagged {
		t.Fatalf("converter wrote no releases key; the dormancy pin would prove nothing")
	}

	findings := BuildSupplyChainImpactFindings(append(envelopes, debianCurl("12", "7.88.1-10+deb12u2").envelope()))
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
	}
	assertSupplyChainImpactStatus(t, findings[0], SupplyChainImpactPossiblyAffected)
	if findings[0].MatchReason != supplyChainVersionReasonOSReleaseUnverified {
		t.Fatalf("MatchReason = %q, want %q (the PR-2a verdict)", findings[0].MatchReason, supplyChainVersionReasonOSReleaseUnverified)
	}
}
