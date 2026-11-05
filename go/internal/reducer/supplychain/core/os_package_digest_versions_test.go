// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// digestVersionRow binds one installed debian curl row to a scan scope that a
// sibling analysis resolves to a digest, the shape #10057 is about: one image
// digest holding more than one installed version under one package key.
func digestVersionRow(version, factID, scope, generation string) facts.Envelope {
	envelope := debianCurl("12", version).envelope()
	envelope.FactID, envelope.ScopeID, envelope.GenerationID = factID, scope, generation
	return envelope
}

// digestVersionsFixture is one digest, one scan scope, and the sibling
// analysis that resolves the scope to the digest.
func digestVersionsFixture(seed int) (digest, scope, generation string, analysis facts.Envelope) {
	digest = fmt.Sprintf("sha256:%064x", seed)
	scope = fmt.Sprintf("image://digest-versions/%d", seed)
	generation = fmt.Sprintf("gen-%d", seed)
	return digest, scope, generation, scannerWorkerAnalysisFact(scope, generation, digest, "registry.example/app@"+digest)
}

// debianCurlFixedAt1_0_4 is a vendor-sourced debian-12 curl fact that names
// 1:1.0-4 as the fixed version and carries no exact-version list.
func debianCurlFixedAt1_0_4() facts.Envelope {
	return facts.Envelope{
		FactID: "affected-dsa", FactKind: chain.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id": vendorLineCVE, "advisory_id": "DSA-10057-1", "source": "debian", "ecosystem": "deb",
			"package_id": "pkg:deb/debian/curl", "package_name": "curl",
			"purl": "pkg:deb/debian/curl?distro=debian-12", "fixed_versions": []any{"1:1.0-4"},
		},
	}
}

// findingAtVersion returns the one finding observed at version and fails when
// there is none or more than one.
func findingAtVersion(t *testing.T, findings []SupplyChainImpactFinding, version string) SupplyChainImpactFinding {
	t.Helper()
	var matched []SupplyChainImpactFinding
	for _, finding := range findings {
		if finding.ObservedVersion == version {
			matched = append(matched, finding)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("findings at %s = %d, want 1: %#v", version, len(matched), findings)
	}
	return matched[0]
}

// TestDigestHoldingTwoVersionsYieldsOneFindingPerVersion pins #10057. One
// image digest holds curl at 1:1.0-3 and 1:1.0-4 under one package key. The
// digest path used to classify the digest once, over whichever row arrived
// first, so a digest holding a version below the fix could read
// not_affected_known_fixed in one row order and possibly_affected in the other.
// Each installed version now gets its own finding and its own verdict, and the
// findings agree to the byte in both orders.
func TestDigestHoldingTwoVersionsYieldsOneFindingPerVersion(t *testing.T) {
	t.Parallel()
	digest, scope, generation, analysis := digestVersionsFixture(0x57)
	rows := []facts.Envelope{
		digestVersionRow("1:1.0-3", "os-v3", scope, generation),
		digestVersionRow("1:1.0-4", "os-v4", scope, generation),
	}
	cve := vulnerabilityCVEFact("cve-9945", vendorLineCVE, 7.5)
	cases := []struct {
		name     string
		advisory []facts.Envelope
		// want maps each installed version to the status its finding must read.
		want map[string]SupplyChainImpactStatus
	}{
		{
			name:     "exact-version fact names the low version",
			advisory: []facts.Envelope{cve, withDistroLine(vendorLineScenarios[0].affectedEnvelope(""), "debian-12"), analysis},
			want: map[string]SupplyChainImpactStatus{
				"1:1.0-3": SupplyChainImpactAffectedExact,
				"1:1.0-4": SupplyChainImpactPossiblyAffected,
			},
		},
		{
			name:     "fix at the high version",
			advisory: []facts.Envelope{cve, debianCurlFixedAt1_0_4(), analysis},
			want: map[string]SupplyChainImpactStatus{
				"1:1.0-3": SupplyChainImpactPossiblyAffected,
				"1:1.0-4": SupplyChainImpactNotAffectedKnownFixed,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings := assertFleetOrderIndependent(t, tc.advisory, rows)
			if len(findings) != len(tc.want) {
				t.Fatalf("findings = %d, want %d (one per installed version): %#v", len(findings), len(tc.want), findings)
			}
			for version, status := range tc.want {
				got := findingAtVersion(t, findings, version)
				if got.Status != status {
					t.Fatalf("version %s status = %q (%s), want %q", version, got.Status, got.MatchReason, status)
				}
				if got.SubjectDigest != digest {
					t.Fatalf("version %s subject digest = %q, want %q", version, got.SubjectDigest, digest)
				}
			}
			// The digest holds a version below the fix, so no finding for that
			// version may read known fixed whatever order the rows arrived in.
			if got := findingAtVersion(t, findings, "1:1.0-3"); got.Status == SupplyChainImpactNotAffectedKnownFixed {
				t.Fatalf("version 1:1.0-3 reads %q", got.Status)
			}
		})
	}
}

// TestDigestScannedTwiceAtOneVersionKeepsOneFinding keeps the digest path from
// fanning out past one finding per (release line, version): two scans of one
// digest that installed the same version are one fact about the image.
func TestDigestScannedTwiceAtOneVersionKeepsOneFinding(t *testing.T) {
	t.Parallel()
	digest, scope, generation, analysis := digestVersionsFixture(0x58)
	scopeTwo, generationTwo := scope+"/rescan", generation+"-rescan"
	analysisTwo := scannerWorkerAnalysisFact(scopeTwo, generationTwo, digest, "registry.example/app@"+digest)
	advisory := []facts.Envelope{
		vulnerabilityCVEFact("cve-9945", vendorLineCVE, 7.5),
		withDistroLine(vendorLineScenarios[0].affectedEnvelope(""), "debian-12"),
		analysis, analysisTwo,
	}
	rows := []facts.Envelope{
		digestVersionRow("1:1.0-3", "os-first", scope, generation),
		digestVersionRow("1:1.0-3", "os-second", scopeTwo, generationTwo),
	}
	findings := assertFleetOrderIndependent(t, advisory, rows)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
	}
	if findings[0].SubjectDigest != digest || findings[0].Status != SupplyChainImpactAffectedExact {
		t.Fatalf("finding = %s/%s, want %s/affected_exact", findings[0].SubjectDigest, findings[0].Status, digest)
	}
}
