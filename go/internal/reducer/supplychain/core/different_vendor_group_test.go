// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	chain "github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// RED for the different-vendor group limit tracked in #10034: a group whose
// facts join rows of different vendors keeps the group, because the anchor is
// judged over the whole index while verdicts are per shard. A live fact
// joining only one shard still anchors the group on the other, so the other
// shard reads the live record instead of the retracted one that joins it.
//
// Shape: one CVE group, one package group with mixed purls (deb + apk, so the
// lookup keys span both vendors' rows), a live Debian record joining the
// Debian row and a withdrawn Alpine record joining the Alpine row. Each shard
// must read the record that anchors it: the Debian shard the live record with
// no retraction, the Alpine shard the retracted record with its retraction.

const (
	shardCVE      = "CVE-2023-9992"
	shardDebian   = "DSA-9992-1"
	shardAlpine   = "ALPINE-9992-1"
	shardDebianV  = "7.88.1-10"
	shardAlpineV  = "8.0.0-r0"
	shardDebianID = "pkg:deb/debian/curl"
	shardApkPURL  = "pkg:apk/alpine/curl"
	shardDebPURL  = "pkg:deb/debian/curl?arch=amd64&distro=debian-12"
)

// shardDebianLive is the live Debian record's affected fact: deb purl, joins
// the Debian row, never the Alpine row.
func shardDebianLive() facts.Envelope {
	return facts.Envelope{FactID: "affected-debian", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
		"cve_id": shardCVE, "advisory_id": shardDebian, "source": "debian",
		"ecosystem": "os", "package_id": shardDebianID, "package_name": "curl",
		"purl":              shardDebPURL,
		"affected_versions": []any{shardDebianV},
	}}
}

// shardAlpineWithdrawn is the withdrawn Alpine record's affected fact: apk
// purl, joins the Alpine row, never the Debian row.
func shardAlpineWithdrawn() facts.Envelope {
	return facts.Envelope{FactID: "affected-alpine", FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
		"cve_id": shardCVE, "advisory_id": shardAlpine, "source": "alpine",
		"ecosystem": "os", "package_id": shardDebianID, "package_name": "curl",
		"purl":              shardApkPURL,
		"affected_versions": []any{shardAlpineV},
	}}
}

// TestDifferentVendorGroupJudgesAnchorPerShard is the limit: the anchor is
// judged on each shard's rows, not the whole index.
func TestDifferentVendorGroupJudgesAnchorPerShard(t *testing.T) {
	t.Parallel()
	envelopes := []facts.Envelope{
		vulnerabilityCVEFactWithProvenance("cve-debian", shardCVE, "debian", shardDebian, 7.5, "", "HIGH", "2026-10-01T00:00:00Z"),
		vulnerabilityCVEFactWithdrawn("cve-alpine", shardCVE, "alpine", shardAlpine, 7.5, "HIGH", "2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z"),
		shardDebianLive(),
		shardAlpineWithdrawn(),
		debianCurl("12", shardDebianV).envelope(),
		alpineCurl("3.18", shardAlpineV).envelope(),
	}
	var first []SupplyChainImpactFinding
	for _, order := range everyOrder(envelopes) {
		findings := BuildSupplyChainImpactFindings(order)
		if len(findings) != 2 {
			t.Fatalf("order %s: findings = %d, want 2 (one per vendor shard): %#v", repDirectOrderLabel(order), len(findings), findings)
		}
		byAdvisory := map[string]SupplyChainImpactFinding{}
		for _, finding := range findings {
			byAdvisory[finding.AdvisoryID] = finding
		}
		debian, ok := byAdvisory[shardDebian]
		if !ok {
			t.Fatalf("order %s: no finding names the live Debian record: %s", repDirectOrderLabel(order), repDirectSummary(findings))
		}
		if debian.AdvisoryRetraction != nil {
			t.Errorf("order %s: Debian shard shows a retraction: %#v", repDirectOrderLabel(order), debian.AdvisoryRetraction)
		}
		alpine, ok := byAdvisory[shardAlpine]
		if !ok {
			t.Fatalf("order %s: no finding names the retracted Alpine record: %s", repDirectOrderLabel(order), repDirectSummary(findings))
		}
		if alpine.AdvisoryRetraction == nil {
			t.Errorf("order %s: Alpine shard shows no retraction: %s", repDirectOrderLabel(order), repDirectSummary(findings))
		}
		if first == nil {
			first = findings
		} else if repDirectSummary(findings) != repDirectSummary(first) {
			t.Fatalf("order %s reads %s, want %s", repDirectOrderLabel(order), repDirectSummary(findings), repDirectSummary(first))
		}
	}
}
