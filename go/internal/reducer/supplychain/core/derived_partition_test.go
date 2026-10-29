// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

func TestAdvisoryMatchesTrustedSource(t *testing.T) {
	envelopeFor := func(kind string, payload map[string]any) facts.Envelope {
		return facts.Envelope{
			FactKind:      kind,
			SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1,
			Payload:       payload,
		}
	}
	dependencyFor := func(class string) AdvisoryAuthorityDependency {
		return AdvisoryAuthorityDependency{Kind: "public_registration", TrustedSourceClass: class, ContentProfile: "supply_chain_impact_v1"}
	}
	cases := []struct {
		name       string
		envelope   facts.Envelope
		dependency AdvisoryAuthorityDependency
		want       bool
	}{
		{
			name:       "positive: kev fact matches cisa kev",
			envelope:   envelopeFor(chain.VulnerabilityKnownExploitedFactKind, map[string]any{"cve_id": "CVE-2024-0001", "source": "cisa_kev"}),
			dependency: dependencyFor("cisa_kev"),
			want:       true,
		},
		{
			name:       "positive: epss fact matches first epss",
			envelope:   envelopeFor(chain.VulnerabilityEPSSScoreFactKind, map[string]any{"cve_id": "CVE-2024-0001", "source": "first_epss"}),
			dependency: dependencyFor("first_epss"),
			want:       true,
		},
		{
			name:       "positive: cve fact matches its seam-decoded source",
			envelope:   envelopeFor(chain.VulnerabilityCVEFactKind, map[string]any{"advisory_id": "GHSA-1", "source": "nvd"}),
			dependency: dependencyFor("nvd"),
			want:       true,
		},
		{
			name:       "positive: source comparison folds case and space",
			envelope:   envelopeFor(chain.VulnerabilityCVEFactKind, map[string]any{"advisory_id": "GHSA-1", "source": " NVD "}),
			dependency: dependencyFor("nvd"),
			want:       true,
		},
		{
			name:       "negative: kev fact does not match osv",
			envelope:   envelopeFor(chain.VulnerabilityKnownExploitedFactKind, map[string]any{"cve_id": "CVE-2024-0001", "source": "cisa_kev"}),
			dependency: dependencyFor("osv"),
			want:       false,
		},
		{
			name:       "negative: cve fact from another source does not match",
			envelope:   envelopeFor(chain.VulnerabilityCVEFactKind, map[string]any{"advisory_id": "GHSA-1", "source": "nvd"}),
			dependency: dependencyFor("osv"),
			want:       false,
		},
		{
			name:       "negative: cve without a source fails closed",
			envelope:   envelopeFor(chain.VulnerabilityCVEFactKind, map[string]any{"advisory_id": "GHSA-1"}),
			dependency: dependencyFor("nvd"),
			want:       false,
		},
		{
			name:       "negative: malformed cve fails closed",
			envelope:   envelopeFor(chain.VulnerabilityCVEFactKind, map[string]any{"source": "nvd"}),
			dependency: dependencyFor("nvd"),
			want:       false,
		},
		{
			name:       "ambiguous: unknown kind fails closed",
			envelope:   envelopeFor(chain.VulnerabilityReferenceFactKind, map[string]any{"source": "nvd"}),
			dependency: dependencyFor("nvd"),
			want:       false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := advisoryMatchesTrustedSource(tc.envelope, tc.dependency); got != tc.want {
				t.Fatalf("advisoryMatchesTrustedSource() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAuthorizedScanImpactPartitionSameDigest(t *testing.T) {
	const (
		firstTarget  = "image://derived/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		secondTarget = "image://derived/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		digest       = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	)
	advisory := derivedPartitionAdvisory("public", "vuln-intel://nvd/cve", "3.0.11-1~deb12u3")
	authority := []AdvisoryAuthorityDependency{{Kind: "public_registration", ID: "registration-1", RevisionID: "revision-1", AdvisoryScopeID: advisory[0].ScopeID, AdvisoryGenerationID: advisory[0].GenerationID, ConfigFingerprint: "config-1", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"}}
	first := derivedPartitionScan("first", firstTarget, "generation-a", digest, "3.0.11-1~deb12u2")
	second := derivedPartitionScan("second", secondTarget, "generation-b", digest, "3.0.11-1~deb12u3")
	var forwardIDs []string
	for _, reverse := range []bool{false, true} {
		targets := []AuthorizedScanImpactEvidence{
			{ScanTargetID: firstTarget, ScanGenerationID: "generation-a", OSPackages: []facts.Envelope{first[0]}, Analyses: []facts.Envelope{first[1]}, Advisories: advisory, AuthorityDependencies: authority, Complete: true},
			{ScanTargetID: secondTarget, ScanGenerationID: "generation-b", OSPackages: []facts.Envelope{second[0]}, Analyses: []facts.Envelope{second[1]}, Advisories: advisory, AuthorityDependencies: authority, Complete: true},
		}
		if reverse {
			slices.Reverse(targets)
		}
		findings, err := BuildAuthorizedScanImpactFindings(nil, targets)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 2 {
			t.Fatalf("reverse=%t got %d findings, want two target-local findings: %#v", reverse, len(findings), findings)
		}
		ids := []string{supplyChainImpactFindingID(findings[0]), supplyChainImpactFindingID(findings[1])}
		if reverse && !slices.Equal(ids, forwardIDs) {
			t.Fatalf("target order changed finding order: %v -> %v", forwardIDs, ids)
		}
		if !reverse {
			forwardIDs = ids
		}
		byTarget := map[string]SupplyChainImpactFinding{}
		for _, finding := range findings {
			byTarget[finding.ScanTargetID] = finding
		}
		if byTarget[firstTarget].Status != SupplyChainImpactAffectedExact || byTarget[secondTarget].Status != SupplyChainImpactNotAffectedKnownFixed {
			t.Fatalf("reverse=%t wrong target statuses: %#v", reverse, byTarget)
		}
		if slices.Contains(byTarget[firstTarget].EvidenceFactIDs, second[0].FactID) || slices.Contains(byTarget[secondTarget].EvidenceFactIDs, first[0].FactID) {
			t.Fatal("scan evidence crossed target boundary")
		}
		if len(byTarget[firstTarget].AuthorityDependencies) != 1 || byTarget[firstTarget].ScanGenerationID != "generation-a" {
			t.Fatal("protected target authority was not retained for writer transaction")
		}
		payload := supplyChainImpactPayload(SupplyChainImpactWrite{}, byTarget[firstTarget])
		if _, leaked := payload["scan_target_id"]; leaked {
			t.Fatal("protected target ID leaked into public finding payload")
		}
		if _, leaked := payload["advisory_authority_dependencies"]; leaked {
			t.Fatal("protected advisory authority leaked into public finding payload")
		}
		if supplyChainImpactFindingID(byTarget[firstTarget]) == supplyChainImpactFindingID(byTarget[secondTarget]) ||
			supplyChainImpactFactID(SupplyChainImpactWrite{ScopeID: "intent", GenerationID: "generation"}, byTarget[firstTarget]) == supplyChainImpactFactID(SupplyChainImpactWrite{ScopeID: "intent", GenerationID: "generation"}, byTarget[secondTarget]) ||
			supplyChainImpactStableFactKey(SupplyChainImpactWrite{}, byTarget[firstTarget]) == supplyChainImpactStableFactKey(SupplyChainImpactWrite{}, byTarget[secondTarget]) {
			t.Fatal("finding identity, fact ID, or stable key collapsed distinct targets")
		}
	}
}

func TestAuthorizedScanImpactConflictingFixedVersions(t *testing.T) {
	const targetID = "image://derived/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const digest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	public := derivedPartitionAdvisory("public", "vuln-intel://nvd/cve", "3.0.11-1~deb12u3")
	private := derivedPartitionAdvisory("private", "private://tenant-a/advisory", "3.0.11-1~deb12u4")
	scan := derivedPartitionScan("first", targetID, "generation-a", digest, "3.0.11-1~deb12u3")
	dependencies := []AdvisoryAuthorityDependency{
		{Kind: "public_registration", ID: "registration-1", RevisionID: "revision-1", AdvisoryScopeID: public[0].ScopeID, AdvisoryGenerationID: public[0].GenerationID, ConfigFingerprint: "config-1", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"},
		{Kind: "private_binding", ScanTargetID: targetID, ID: "binding-1", RevisionID: "revision-1", AdvisoryScopeID: private[0].ScopeID, AdvisoryGenerationID: private[0].GenerationID, ConfigFingerprint: "config-2", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"},
	}
	for _, reverse := range []bool{false, true} {
		advisories := append(slices.Clone(public), private...)
		if reverse {
			slices.Reverse(advisories)
		}
		findings, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{{
			ScanTargetID: targetID, ScanGenerationID: "generation-a",
			OSPackages: []facts.Envelope{scan[0]}, Analyses: []facts.Envelope{scan[1]},
			Advisories: advisories, AuthorityDependencies: dependencies, Complete: true,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("reverse=%t findings=%d, want one", reverse, len(findings))
		}
		finding := findings[0]
		if finding.Status != SupplyChainImpactPossiblyAffected || finding.Confidence != "partial" || finding.MatchReason != "advisory_fixed_version_conflict" || finding.FixedVersion != "" {
			t.Fatalf("reverse=%t disputed fix became definitive: status=%s confidence=%s reason=%s fixed=%s", reverse, finding.Status, finding.Confidence, finding.MatchReason, finding.FixedVersion)
		}
		if strings.Contains(finding.PriorityReason, "affected_exact") || finding.Remediation.FirstPatchedVersion != "" {
			t.Fatalf("reverse=%t stale priority or remediation selected a disputed fix", reverse)
		}
		for _, envelope := range []facts.Envelope{public[1], private[1]} {
			if !slices.Contains(finding.EvidenceFactIDs, envelope.FactID) {
				t.Fatalf("reverse=%t missing conflicting provenance %s", reverse, envelope.FactID)
			}
		}
	}
}

func TestAuthorizedScanImpactDisjointAffectedBranches(t *testing.T) {
	const targetID = "image://derived/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	public := derivedPartitionAdvisory("public", "vuln-intel://nvd/cve", "3.0.11-1~deb12u3")
	private := derivedPartitionAdvisory("private", "private://tenant-a/advisory", "3.0.11-1~deb12u6")
	public[1].Payload["affected_ranges"] = []any{map[string]any{"type": "ECOSYSTEM", "events": []any{
		map[string]any{"introduced": "3.0.11-1~deb12u1"}, map[string]any{"fixed": "3.0.11-1~deb12u3"},
	}}}
	private[1].Payload["affected_versions"] = []any{"3.0.11-1~deb12u5"}
	private[1].Payload["affected_ranges"] = []any{map[string]any{"type": "ECOSYSTEM", "events": []any{
		map[string]any{"introduced": "3.0.11-1~deb12u4"}, map[string]any{"fixed": "3.0.11-1~deb12u6"},
	}}}
	scan := derivedPartitionScan("first", targetID, "generation-a", "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "3.0.11-1~deb12u2")
	input := AuthorizedScanImpactEvidence{
		ScanTargetID: targetID, ScanGenerationID: "generation-a", OSPackages: []facts.Envelope{scan[0]}, Analyses: []facts.Envelope{scan[1]},
		Advisories: append(public, private...), AuthorityDependencies: []AdvisoryAuthorityDependency{
			{Kind: "public_registration", ID: "registration-1", RevisionID: "revision-1", AdvisoryScopeID: public[0].ScopeID, AdvisoryGenerationID: public[0].GenerationID, ConfigFingerprint: "config-1", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"},
			{Kind: "private_binding", ScanTargetID: targetID, ID: "binding-1", RevisionID: "revision-1", AdvisoryScopeID: private[0].ScopeID, AdvisoryGenerationID: private[0].GenerationID, ConfigFingerprint: "config-2", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"},
		}, Complete: true,
	}
	findings, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{input})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Status != SupplyChainImpactAffectedExact || findings[0].FixedVersion != "3.0.11-1~deb12u3" {
		t.Fatalf("disjoint branches should select the applicable exact match and fix: %#v", findings)
	}
	// An explicit version outside the first branch range overlaps the second
	// branch, even though the ranges themselves do not overlap.
	public[1].Payload["affected_versions"] = []any{"3.0.11-1~deb12u5"}
	private[1].Payload["affected_versions"] = []any{"3.0.11-1~deb12u4"}
	scan = derivedPartitionScan("overlap", targetID, "generation-a", "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "3.0.11-1~deb12u5")
	input.OSPackages = []facts.Envelope{scan[0]}
	input.Analyses = []facts.Envelope{scan[1]}
	findings, err = BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{input})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Status != SupplyChainImpactPossiblyAffected || findings[0].MatchReason != "advisory_fixed_version_conflict" {
		t.Fatalf("explicit affected version outside its range hid a conflicting branch: %#v", findings)
	}
}

func TestAuthorizedScanImpactRejectsUntrustedOrStaleEvidence(t *testing.T) {
	const targetID = "image://derived/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	advisory := derivedPartitionAdvisory("public", "vuln-intel://nvd/cve", "3.0.11-1~deb12u3")
	scan := derivedPartitionScan("first", targetID, "generation-a", "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "3.0.11-1~deb12u2")
	base := AuthorizedScanImpactEvidence{
		ScanTargetID: targetID, ScanGenerationID: "generation-a", OSPackages: []facts.Envelope{scan[0]}, Analyses: []facts.Envelope{scan[1]},
		Advisories: advisory, AuthorityDependencies: []AdvisoryAuthorityDependency{{Kind: "public_registration", ID: "registration-1", RevisionID: "revision-1", AdvisoryScopeID: advisory[0].ScopeID, AdvisoryGenerationID: advisory[0].GenerationID, ConfigFingerprint: "config-1", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"}}, Complete: true,
	}
	for name, mutate := range map[string]func(*AuthorizedScanImpactEvidence){
		"missing authority": func(target *AuthorizedScanImpactEvidence) { target.AuthorityDependencies = nil },
		"public dependency target scoped": func(target *AuthorizedScanImpactEvidence) {
			target.AuthorityDependencies = slices.Clone(target.AuthorityDependencies)
			target.AuthorityDependencies[0].ScanTargetID = targetID
		},
		"private sibling target": func(target *AuthorizedScanImpactEvidence) {
			target.AuthorityDependencies = []AdvisoryAuthorityDependency{{Kind: "private_binding", ScanTargetID: "image://derived/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ID: "binding-1", RevisionID: "revision-1", AdvisoryScopeID: advisory[0].ScopeID, AdvisoryGenerationID: advisory[0].GenerationID, ConfigFingerprint: "config-1", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"}}
		},
		"incomplete load":       func(target *AuthorizedScanImpactEvidence) { target.Complete = false },
		"stale scan generation": func(target *AuthorizedScanImpactEvidence) { target.ScanGenerationID = "generation-b" },
		"unsupported trusted source": func(target *AuthorizedScanImpactEvidence) {
			target.AuthorityDependencies = slices.Clone(target.AuthorityDependencies)
			target.AuthorityDependencies[0].TrustedSourceClass = "custom-unmatched"
			target.Advisories = slices.Clone(target.Advisories)
			for i := range target.Advisories {
				target.Advisories[i] = target.Advisories[i].Clone()
				target.Advisories[i].Payload["source"] = "custom-unmatched"
			}
		},
		"conflicting duplicate fact": func(target *AuthorizedScanImpactEvidence) {
			altered := target.Advisories[1].Clone()
			altered.Payload["fixed_versions"] = []any{"3.0.11-1~deb12u9"}
			target.Advisories = append(slices.Clone(target.Advisories), altered)
		},
		"malformed scan fact": func(target *AuthorizedScanImpactEvidence) {
			target.OSPackages = slices.Clone(target.OSPackages)
			target.OSPackages[0] = target.OSPackages[0].Clone()
			delete(target.OSPackages[0].Payload, "name")
		},
		"malformed advisory": func(target *AuthorizedScanImpactEvidence) {
			target.Advisories = slices.Clone(target.Advisories)
			target.Advisories[1] = target.Advisories[1].Clone()
			delete(target.Advisories[1].Payload, "advisory_id")
		},
		"untrusted source label": func(target *AuthorizedScanImpactEvidence) {
			target.Advisories = slices.Clone(target.Advisories)
			target.Advisories[1] = target.Advisories[1].Clone()
			target.Advisories[1].Payload["source"] = "nvd"
		},
	} {
		t.Run(name, func(t *testing.T) {
			target := base
			mutate(&target)
			if _, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{target}); err == nil {
				t.Fatal("untrusted or stale target evidence was classified")
			}
		})
	}
}

func TestAuthorizedScanImpactCompleteEmptyAdvisoryGeneration(t *testing.T) {
	const targetID = "image://derived/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	advisory := derivedPartitionAdvisory("empty", "vuln-intel://nvd/cve", "3.0.11-1~deb12u3")
	scan := derivedPartitionScan("empty", targetID, "generation-a", "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "3.0.11-1~deb12u2")
	findings, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{{
		ScanTargetID: targetID, ScanGenerationID: "generation-a",
		OSPackages: []facts.Envelope{scan[0]}, Analyses: []facts.Envelope{scan[1]},
		AuthorityDependencies: []AdvisoryAuthorityDependency{{
			Kind: "public_registration", ID: "registration-empty", RevisionID: "revision-empty",
			AdvisoryScopeID: advisory[0].ScopeID, AdvisoryGenerationID: advisory[0].GenerationID,
			ConfigFingerprint: "config-empty", ProviderID: "debian", TrustedSourceClass: "debian",
			ContentProfile: "supply_chain_impact_v1",
		}}, Complete: true,
	}})
	if err != nil || len(findings) != 0 {
		t.Fatalf("complete authorized empty advisory generation: findings=%d err=%v", len(findings), err)
	}
}

func TestAuthorizedScanImpactDuplicateDeliveryAndStableIdentity(t *testing.T) {
	const targetID = "image://derived/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	advisory := derivedPartitionAdvisory("public", "vuln-intel://nvd/cve", "3.0.11-1~deb12u3")
	scan := derivedPartitionScan("first", targetID, "generation-a", "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "3.0.11-1~deb12u2")
	authority := []AdvisoryAuthorityDependency{{Kind: "public_registration", ID: "registration-1", RevisionID: "revision-1", AdvisoryScopeID: advisory[0].ScopeID, AdvisoryGenerationID: advisory[0].GenerationID, ConfigFingerprint: "config-1", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"}}
	var prior SupplyChainImpactFinding
	for _, generation := range []string{"generation-a", "generation-b"} {
		replayed := []facts.Envelope{scan[0].Clone(), scan[1].Clone()}
		for i := range replayed {
			replayed[i].GenerationID = generation
		}
		findings, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{{
			ScanTargetID: targetID, ScanGenerationID: generation,
			OSPackages: []facts.Envelope{replayed[0], replayed[0]}, Analyses: []facts.Envelope{replayed[1], replayed[1]},
			Advisories: append(slices.Clone(advisory), advisory...), AuthorityDependencies: append(slices.Clone(authority), authority...), Complete: true,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("generation=%s duplicate delivery produced %d findings", generation, len(findings))
		}
		if len(findings[0].AuthorityDependencies) != 1 {
			t.Fatalf("generation=%s duplicate authority dependency retained %d times", generation, len(findings[0].AuthorityDependencies))
		}
		if prior.ScanTargetID != "" && (supplyChainImpactFindingID(prior) != supplyChainImpactFindingID(findings[0]) ||
			supplyChainImpactStableFactKey(SupplyChainImpactWrite{}, prior) != supplyChainImpactStableFactKey(SupplyChainImpactWrite{}, findings[0])) {
			t.Fatal("attempt generation changed stable target finding identity")
		}
		prior = findings[0]
	}
}

func TestAuthorizedScanImpactRejectsUnboundDerivedScope(t *testing.T) {
	legacy := []SupplyChainImpactFinding{{
		ScanTargetID: "image://derived/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}}
	if _, err := BuildAuthorizedScanImpactFindings(legacy, nil); err == nil {
		t.Fatal("derived finding was accepted as classified legacy input")
	}
}

func TestAuthorizedScanImpactPublicPrivateExtensionIsolation(t *testing.T) {
	const firstTarget = "image://derived/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const secondTarget = "image://derived/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const digest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	public := derivedPartitionAdvisory("public", "vuln-intel://nvd/cve", "3.0.11-1~deb12u3")
	private := derivedPartitionAdvisory("private", "private://tenant-a/advisory", "3.0.11-1~deb12u4")
	extension := derivedPartitionAdvisory("extension", "extension://tenant-b/advisory", "3.0.11-1~deb12u5")
	first := derivedPartitionScan("first", firstTarget, "generation-a", digest, "3.0.11-1~deb12u3")
	second := derivedPartitionScan("second", secondTarget, "generation-b", digest, "3.0.11-1~deb12u3")
	publicAuthority := AdvisoryAuthorityDependency{Kind: "public_registration", ID: "registration-1", RevisionID: "revision-1", AdvisoryScopeID: public[0].ScopeID, AdvisoryGenerationID: public[0].GenerationID, ConfigFingerprint: "config-public", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"}
	firstAuthority := AdvisoryAuthorityDependency{Kind: "private_binding", ScanTargetID: firstTarget, ID: "binding-first", RevisionID: "revision-first", AdvisoryScopeID: private[0].ScopeID, AdvisoryGenerationID: private[0].GenerationID, ConfigFingerprint: "config-first", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"}
	secondAuthority := AdvisoryAuthorityDependency{Kind: "private_binding", ScanTargetID: secondTarget, ID: "binding-second", RevisionID: "revision-second", AdvisoryScopeID: extension[0].ScopeID, AdvisoryGenerationID: extension[0].GenerationID, ConfigFingerprint: "config-second", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"}
	for _, reverse := range []bool{false, true} {
		firstAdvisories := append(slices.Clone(public), private...)
		secondAdvisories := append(slices.Clone(public), extension...)
		if reverse {
			slices.Reverse(firstAdvisories)
			slices.Reverse(secondAdvisories)
		}
		targets := []AuthorizedScanImpactEvidence{
			{ScanTargetID: firstTarget, ScanGenerationID: "generation-a", OSPackages: []facts.Envelope{first[0]}, Analyses: []facts.Envelope{first[1]}, Advisories: firstAdvisories, AuthorityDependencies: []AdvisoryAuthorityDependency{publicAuthority, firstAuthority}, Complete: true},
			{ScanTargetID: secondTarget, ScanGenerationID: "generation-b", OSPackages: []facts.Envelope{second[0]}, Analyses: []facts.Envelope{second[1]}, Advisories: secondAdvisories, AuthorityDependencies: []AdvisoryAuthorityDependency{publicAuthority, secondAuthority}, Complete: true},
		}
		if reverse {
			slices.Reverse(targets)
		}
		findings, err := BuildAuthorizedScanImpactFindings(nil, targets)
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 2 {
			t.Fatalf("reverse=%t findings=%d, want two", reverse, len(findings))
		}
		for _, finding := range findings {
			if finding.Status != SupplyChainImpactPossiblyAffected || finding.MatchReason != "advisory_fixed_version_conflict" {
				t.Fatalf("reverse=%t target=%s status=%s reason=%s", reverse, finding.ScanTargetID, finding.Status, finding.MatchReason)
			}
			switch finding.ScanTargetID {
			case firstTarget:
				if slices.Contains(finding.EvidenceFactIDs, extension[1].FactID) || !slices.Contains(finding.EvidenceFactIDs, private[1].FactID) || len(finding.AuthorityDependencies) != 2 {
					t.Fatalf("reverse=%t first target advisory boundary/provenance wrong: %#v", reverse, finding)
				}
			case secondTarget:
				if slices.Contains(finding.EvidenceFactIDs, private[1].FactID) || !slices.Contains(finding.EvidenceFactIDs, extension[1].FactID) || len(finding.AuthorityDependencies) != 2 {
					t.Fatalf("reverse=%t second target advisory boundary/provenance wrong: %#v", reverse, finding)
				}
			default:
				t.Fatalf("unexpected scan target %q", finding.ScanTargetID)
			}
		}
	}
}

func TestAuthorizedScanImpactPrivateAdvisoryExcludedFromLegacyPass(t *testing.T) {
	const targetID = "image://derived/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const digest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	public := derivedPartitionAdvisory("public", "vuln-intel://nvd/cve", "3.0.11-1~deb12u3")
	private := derivedPartitionAdvisory("private", "private://tenant-a/advisory", "3.0.11-1~deb12u4")
	legacyScan := derivedPartitionScan("legacy", "image://legacy", "generation-legacy", digest, "3.0.11-1~deb12u3")
	targetScan := derivedPartitionScan("first", targetID, "generation-a", digest, "3.0.11-1~deb12u3")
	legacyFindings := BuildSupplyChainImpactFindings(append(slices.Clone(public), legacyScan...))
	findings, err := BuildAuthorizedScanImpactFindings(legacyFindings, []AuthorizedScanImpactEvidence{{
		ScanTargetID: targetID, ScanGenerationID: "generation-a", OSPackages: []facts.Envelope{targetScan[0]}, Analyses: []facts.Envelope{targetScan[1]},
		Advisories: append(slices.Clone(public), private...), AuthorityDependencies: []AdvisoryAuthorityDependency{
			{Kind: "public_registration", ID: "registration-1", RevisionID: "revision-1", AdvisoryScopeID: public[0].ScopeID, AdvisoryGenerationID: public[0].GenerationID, ConfigFingerprint: "config-1", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"},
			{Kind: "private_binding", ScanTargetID: targetID, ID: "binding-1", RevisionID: "revision-1", AdvisoryScopeID: private[0].ScopeID, AdvisoryGenerationID: private[0].GenerationID, ConfigFingerprint: "config-2", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"},
		}, Complete: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings=%d, want legacy and target rows", len(findings))
	}
	for _, finding := range findings {
		if finding.ScanTargetID == "" && slices.Contains(finding.EvidenceFactIDs, private[1].FactID) {
			t.Fatal("target-private advisory reached legacy global finding")
		}
	}
}

func TestLegacyImpactIdentityUnchangedByDerivedTargetField(t *testing.T) {
	finding := SupplyChainImpactFinding{
		CVEID: "CVE-2026-9299", AdvisoryID: "DSA-2026-9299", PackageID: "pkg:deb/debian/openssl",
		PURL: "pkg:deb/debian/openssl@3.0.11-1~deb12u2", ObservedVersion: "3.0.11-1~deb12u2",
		Status: SupplyChainImpactAffectedExact, SubjectDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
	legacyIdentity := map[string]any{
		"cve_id": finding.CVEID, "advisory_id": finding.AdvisoryID, "package_id": finding.PackageID,
		"purl": finding.PURL, "product_criteria": "", "match_criteria_id": "",
		"observed_version": finding.ObservedVersion, "requested_range": "", "impact_status": string(finding.Status),
		"subject_digest": finding.SubjectDigest, "repository_id": "",
	}
	wantID := supplyChainImpactFactKind + ":" + facts.StableID(supplyChainImpactFactKind, legacyIdentity)
	if got := supplyChainImpactFindingID(finding); got != wantID {
		t.Fatalf("legacy finding ID changed: got %s want %s", got, wantID)
	}
	wantKey := strings.Join([]string{
		"supply_chain_impact", finding.CVEID, finding.AdvisoryID, finding.PackageID,
		finding.PURL, "", "", finding.ObservedVersion, "", string(finding.Status), "", finding.SubjectDigest,
	}, ":")
	if got := supplyChainImpactStableFactKey(SupplyChainImpactWrite{}, finding); got != wantKey {
		t.Fatalf("legacy stable key changed: got %q want %q", got, wantKey)
	}
}

func TestAuthorizedScanImpactDisjointBranchGapStaysPartial(t *testing.T) {
	const targetID = "image://derived/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	public := derivedPartitionAdvisory("public", "vuln-intel://nvd/cve", "3.0.11-1~deb12u3")
	private := derivedPartitionAdvisory("private", "private://tenant-a/advisory", "3.0.11-1~deb12u6")
	public[1].Payload["affected_ranges"] = []any{map[string]any{"type": "ECOSYSTEM", "events": []any{
		map[string]any{"introduced": "3.0.11-1~deb12u1"}, map[string]any{"fixed": "3.0.11-1~deb12u3"},
	}}}
	private[1].Payload["affected_versions"] = []any{"3.0.11-1~deb12u5"}
	private[1].Payload["affected_ranges"] = []any{map[string]any{"type": "ECOSYSTEM", "events": []any{
		map[string]any{"introduced": "3.0.11-1~deb12u4"}, map[string]any{"fixed": "3.0.11-1~deb12u6"},
	}}}
	scan := derivedPartitionScan("gap", targetID, "generation-a", "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "3.0.11-1~deb12u3")
	findings, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{{
		ScanTargetID: targetID, ScanGenerationID: "generation-a", OSPackages: []facts.Envelope{scan[0]}, Analyses: []facts.Envelope{scan[1]},
		Advisories: append(public, private...), AuthorityDependencies: []AdvisoryAuthorityDependency{
			{Kind: "public_registration", ID: "registration-1", RevisionID: "revision-1", AdvisoryScopeID: public[0].ScopeID, AdvisoryGenerationID: public[0].GenerationID, ConfigFingerprint: "config-1", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"},
			{Kind: "private_binding", ScanTargetID: targetID, ID: "binding-1", RevisionID: "revision-1", AdvisoryScopeID: private[0].ScopeID, AdvisoryGenerationID: private[0].GenerationID, ConfigFingerprint: "config-2", ProviderID: "debian", TrustedSourceClass: "debian", ContentProfile: "supply_chain_impact_v1"},
		}, Complete: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Status != SupplyChainImpactPossiblyAffected ||
		findings[0].MatchReason != "advisory_branch_applicability_unknown" || findings[0].FixedVersion != "" {
		t.Fatalf("gap between disjoint branches selected an arbitrary fix: %#v", findings)
	}
}
