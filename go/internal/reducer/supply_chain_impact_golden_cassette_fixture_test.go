// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// This file is the fast, Docker-free framework proof for the B-7
// golden-corpus cassette backfill (epic #5462 order-0 part B): it proves the
// envelope shapes the cassette's "debian-image" scope carries in
// testdata/cassettes/vulnerabilityintelligence/supply-chain-demo.json are
// schema-consistent with the CURRENT reducer and synthesize the intended
// supply-chain-impact truth, without needing the full live gate. The fact
// payloads below mirror the payloads committed in that cassette scope; the
// committed JSON itself is validated by TestCommittedCassettesValid and the
// live B-7 replay, so this file's unique value is the synthesis assertions
// (the derived finding shape and enrichment) the schema gate cannot check.
// This cassette scope carries an os_package fact but NO sibling
// scanner_worker.analysis fact, so after #5463 deleted the
// scope_id-as-SubjectDigest fallback the os_package-backed finding below
// asserts SubjectDigest is BLANK: classifySupplyChainImpactPackage anchors
// SubjectDigest on the real image_digest of a sibling scanner_worker.analysis
// fact joined by ScopeID+GenerationID, and with no such sibling here it must
// leave SubjectDigest empty rather than substituting the opaque scope_id (a
// scan-target locator, never a sha256). The finding itself is still retained
// because a vendor-matched os_package is its own owned anchor
// (supplyChainImpactFindingHasOwnedAnchor). #5464 adds the scanner_worker.
// analysis cassette fact + the queryable real-digest assertion.
const goldenCassetteDebianImageScopeID = "vulnerability_intelligence:supply-chain-demo:debian-image"

// TestGoldenCassetteDebianOSPackageChainSynthesizesFinding proves the
// debian/openssl CVE+affected_package+os_package chain the cassette backfill
// adds produces exactly one image_os_package-backed
// SupplyChainImpactAffectedExact finding, and that the epss_score /
// known_exploited enrichment facts for the same CVE populate the finding's
// risk signals (EPSSProbability, EPSSPercentile, KnownExploited,
// PriorityReason). It also proves the sibling standalone kinds
// (affected_product, reference, source_snapshot, go_module_evidence,
// go_call_reachability, warning) decode without error and without
// perturbing this finding, since they are schema-valid coverage facts the
// current reducer does not join into any finding (affected_product is
// shadowed because this CVE already has an affected_package; reference,
// source_snapshot, and warning have no case in
// addSupplyChainImpactIndexEntry; go_module_evidence/go_call_reachability
// require a gomod-ecosystem affected_package to key against, which this
// debian/deb chain does not provide).
func TestGoldenCassetteDebianOSPackageChainSynthesizesFinding(t *testing.T) {
	t.Parallel()

	envelopes := goldenCassetteDebianImageChainEnvelopes()

	findings := BuildSupplyChainImpactFindings(envelopes)

	var got *SupplyChainImpactFinding
	for i := range findings {
		if findings[i].CVEID == "CVE-2026-00010" {
			got = &findings[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("no finding for CVE-2026-00010 among %d findings: %#v", len(findings), findings)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want exactly 1 (the affected_product fact for the same CVE must not "+
			"produce a second finding while an affected_package finding already exists): %#v",
			len(findings), findings)
	}

	assertSupplyChainImpactStatus(t, *got, SupplyChainImpactAffectedExact)
	if got.RuntimeReachability != "image_os_package" {
		t.Fatalf("RuntimeReachability = %q, want image_os_package", got.RuntimeReachability)
	}
	// DPKG-matched findings classify as DetectionProfileComprehensive, not
	// precise: classifySupplyChainImpactDetectionProfile
	// (supply_chain_impact_profile.go) only grants
	// DetectionProfilePrecise to a fixed set of MatchReason values that does
	// not include the dpkg affected-range reason (only RPM's exact-affected
	// reason is precise among the OS-package matchers).
	if got.DetectionProfile != DetectionProfileComprehensive {
		t.Fatalf("DetectionProfile = %q, want comprehensive (dpkg matches are not in the precise reason set)", got.DetectionProfile)
	}
	if got.SubjectDigest != "" {
		t.Fatalf("SubjectDigest = %q, want empty: this cassette scope carries no "+
			"scanner_worker.analysis sibling fact, and #5463 deleted the scope_id-as-SubjectDigest "+
			"fallback, so an os_package finding with no real image digest leaves SubjectDigest blank "+
			"rather than substituting the scope_id %q", got.SubjectDigest, goldenCassetteDebianImageScopeID)
	}
	if got.ObservedVersion != "3.0.11-1~deb12u2" {
		t.Fatalf("ObservedVersion = %q, want the installed dpkg EVR", got.ObservedVersion)
	}
	if got.FixedVersionSource != "debian" {
		t.Fatalf("FixedVersionSource = %q, want debian", got.FixedVersionSource)
	}
	path := strings.Join(got.EvidencePath, " -> ")
	if !strings.Contains(path, facts.VulnerabilityOSPackageFactKind) {
		t.Fatalf("EvidencePath = %#v, want os package evidence", got.EvidencePath)
	}

	// Enrichment: epss_score + known_exploited populate risk signals for
	// every finding keyed on this CVE, including the os_package-backed one
	// (applyRiskSignals, supply_chain_impact_product.go), regardless of
	// which extraction path produced the finding.
	if got.EPSSProbability != "0.87345" {
		t.Fatalf("EPSSProbability = %q, want 0.87345 from the epss_score fact", got.EPSSProbability)
	}
	if got.EPSSPercentile != "0.99012" {
		t.Fatalf("EPSSPercentile = %q, want 0.99012 from the epss_score fact", got.EPSSPercentile)
	}
	if !got.KnownExploited {
		t.Fatalf("KnownExploited = false, want true from the known_exploited fact")
	}
	// withSupplyChainImpactPriority (supply_chain_impact_priority.go)
	// overwrites PriorityReason with the final triage-bucket sentence after
	// applyRiskSignals sets its own interim value, so KnownExploited's
	// effect is asserted through PriorityReasonCodes/PriorityContributions
	// instead of PriorityReason text.
	if !containsString(got.PriorityReasonCodes, "cisa_kev") {
		t.Fatalf("PriorityReasonCodes = %#v, want cisa_kev present", got.PriorityReasonCodes)
	}
}

// TestGoldenCassetteDebianImageChainEnvelopesDecodeWithoutQuarantine proves
// every envelope the cassette backfill adds (all 9 previously-missing
// vulnerability_intelligence kinds) decodes cleanly through
// buildSupplyChainImpactFindingsWithQuarantine with zero quarantined facts
// and no fatal error, so none of the new cassette payloads will surface as
// an input_invalid dead-letter or abort the golden-corpus gate's replay.
func TestGoldenCassetteDebianImageChainEnvelopesDecodeWithoutQuarantine(t *testing.T) {
	t.Parallel()

	envelopes := goldenCassetteDebianImageChainEnvelopes()

	findings, quarantined, err := buildSupplyChainImpactFindingsWithQuarantine(envelopes)
	if err != nil {
		t.Fatalf("buildSupplyChainImpactFindingsWithQuarantine() error = %v", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %d, want 0: %#v", len(quarantined), quarantined)
	}
	if len(findings) == 0 {
		t.Fatalf("findings = 0, want at least the debian os_package finding")
	}
}

// goldenCassetteDebianImageChainEnvelopes returns the exact fact set (fact
// kind + payload) the cassette backfill transcribes verbatim into the new
// "vulnerability_intelligence:supply-chain-demo:debian-image" scope. Every
// envelope shares goldenCassetteDebianImageScopeID because a cassette scope
// has no per-fact ScopeID (see the file doc comment above).
func goldenCassetteDebianImageChainEnvelopes() []facts.Envelope {
	const scopeID = goldenCassetteDebianImageScopeID
	return []facts.Envelope{
		{
			FactID:   "debian-cve-scd-00010",
			FactKind: facts.VulnerabilityCVEFactKind,
			ScopeID:  scopeID,
			Payload: map[string]any{
				"collector_instance_id": "collector-vulnintel-scd",
				"source":                "debian",
				"source_schema_version": "1.0",
				"advisory_id":           "DSA-2026-00010",
				"cve_id":                "CVE-2026-00010",
				"aliases":               []any{"CVE-2026-00010", "DSA-2026-00010"},
				"related":               []any{},
				"summary":               "Synthetic supply-chain-demo Debian OpenSSL advisory",
				"details":               "Synthetic Debian security advisory for the golden-corpus replay cassette OS-package chain.",
				"published_at":          "2026-06-20T00:00:00Z",
				"modified_at":           "2026-06-24T00:00:00Z",
				"withdrawn_at":          "",
				"cvss_score":            7.5,
				"cvss_vector":           "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
				"severity_label":        "HIGH",
				"correlation_anchors":   []any{"CVE-2026-00010", "DSA-2026-00010"},
			},
		},
		{
			FactID:   "debian-affected-package-scd-00010",
			FactKind: facts.VulnerabilityAffectedPackageFactKind,
			ScopeID:  scopeID,
			Payload: map[string]any{
				"cve_id":            "CVE-2026-00010",
				"advisory_id":       "DSA-2026-00010",
				"source":            "debian",
				"package_id":        "pkg:deb/debian/openssl",
				"ecosystem":         "deb",
				"package_name":      "openssl",
				"affected_versions": []any{"3.0.11-1~deb12u2"},
				"fixed_versions":    []any{"3.0.11-1~deb12u3"},
			},
		},
		{
			FactID:   "debian-os-package-scd-00010",
			FactKind: facts.VulnerabilityOSPackageFactKind,
			ScopeID:  scopeID,
			Payload: map[string]any{
				"distro":                 "debian",
				"distro_version":         "12",
				"package_manager":        "dpkg",
				"name":                   "openssl",
				"arch":                   "amd64",
				"repository_class":       "vendor",
				"vendor_advisory_source": "debian",
				"installed_version_raw":  "3.0.11-1~deb12u2",
				"purl":                   "pkg:deb/debian/openssl@3.0.11-1~deb12u2?arch=amd64&distro=debian-12",
			},
		},
		{
			FactID:   "debian-epss-scd-00010",
			FactKind: facts.VulnerabilityEPSSScoreFactKind,
			ScopeID:  scopeID,
			Payload: map[string]any{
				"cve_id":      "CVE-2026-00010",
				"probability": "0.87345",
				"percentile":  "0.99012",
				"score_date":  "2026-06-24",
			},
		},
		{
			FactID:   "debian-kev-scd-00010",
			FactKind: facts.VulnerabilityKnownExploitedFactKind,
			ScopeID:  scopeID,
			Payload: map[string]any{
				"cve_id":                        "CVE-2026-00010",
				"catalog_version":               "2026.06.24",
				"catalog_date_released":         "2026-06-24",
				"vendor_project":                "Debian Project",
				"product":                       "OpenSSL",
				"vulnerability_name":            "Synthetic Debian OpenSSL Advisory",
				"date_added":                    "2026-06-25",
				"short_description":             "Synthetic KEV entry for the golden-corpus replay cassette.",
				"required_action":               "Apply the vendor security update per Debian advisory guidance.",
				"due_date":                      "2026-07-15",
				"known_ransomware_campaign_use": "Unknown",
				"cwes":                          []any{"CWE-326"},
			},
		},
		{
			FactID:   "debian-reference-scd-00010",
			FactKind: facts.VulnerabilityReferenceFactKind,
			ScopeID:  scopeID,
			Payload: map[string]any{
				"url":                 "https://example.com/advisories/dsa-2026-00010",
				"advisory_id":         "DSA-2026-00010",
				"cve_id":              "CVE-2026-00010",
				"source":              "debian",
				"reference_type":      "ADVISORY",
				"correlation_anchors": []any{"CVE-2026-00010", "DSA-2026-00010", "https://example.com/advisories/dsa-2026-00010"},
			},
		},
		{
			FactID:   "debian-source-snapshot-scd-00010",
			FactKind: facts.VulnerabilitySourceSnapshotFactKind,
			ScopeID:  scopeID,
			Payload: map[string]any{
				"source":                 "debian",
				"complete":               true,
				"ecosystem":              "os",
				"cache_artifact_version": "vulnerability-source-cache.v1",
				"cache_snapshot_digest":  "sha256:demo-debian-image-00010",
				"cache_updated_at":       "2026-06-24T00:00:00Z",
				"cache_freshness":        "fresh",
				"warning_code":           "",
				"warning_message":        "",
			},
		},
		{
			// Standalone: shadowed by the affected_package fact above for the
			// same CVE (buildSupplyChainImpactFindingsWithQuarantine only
			// consults index.affectedProducts when a CVE has zero
			// affectedPackages entries). Still schema-valid and still raises
			// per-kind coverage.
			FactID:   "debian-affected-product-scd-00010",
			FactKind: facts.VulnerabilityAffectedProductFactKind,
			ScopeID:  scopeID,
			Payload: map[string]any{
				"cve_id":            "CVE-2026-00010",
				"criteria":          "cpe:2.3:a:debian:openssl:3.0.11:*:*:*:*:*:*:*",
				"match_criteria_id": "supply-chain-demo-debian-openssl-00010",
				"vulnerable":        true,
			},
		},
		{
			// Standalone: no gomod-ecosystem affected_package exists in this
			// cassette to key classifyGoVulnerabilityReachabilityWithQuarantine's
			// advisoryByModule lookup against, so this decodes cleanly and
			// raises kind coverage but joins no finding (per the task's
			// explicit "do not force-fit" allowance for the two Go kinds).
			FactID:   "debian-go-module-evidence-scd-00010",
			FactKind: facts.VulnerabilityGoModuleEvidenceFactKind,
			ScopeID:  scopeID,
			Payload: map[string]any{
				"repository_id":    "repository:supply-chain-demo/example-go-service",
				"relative_path":    "go.mod",
				"module_path":      "github.com/eshu-hq/supply-chain-demo-module",
				"required_version": "v1.4.0",
				"indirect":         false,
				"line_number":      9,
			},
		},
		{
			// Standalone, same rationale as go_module_evidence above: no
			// matching go advisory/module pair exists in this cassette.
			FactID:   "debian-go-call-reachability-scd-00010",
			FactKind: facts.VulnerabilityGoCallReachabilityFactKind,
			ScopeID:  scopeID,
			Payload: map[string]any{
				"repository_id":      "repository:supply-chain-demo/example-go-service",
				"osv_id":             "GO-2026-00010",
				"deepest_module":     "github.com/eshu-hq/supply-chain-demo-module",
				"deepest_package":    "github.com/eshu-hq/supply-chain-demo-module/internal/vuln",
				"deepest_symbol":     "Vulnerable",
				"reachability_level": "symbol",
				"trace": []any{
					map[string]any{
						"module":   "github.com/eshu-hq/supply-chain-demo-service",
						"package":  "github.com/eshu-hq/supply-chain-demo-service/cmd/demo",
						"function": "main",
					},
					map[string]any{
						"module":   "github.com/eshu-hq/supply-chain-demo-module",
						"package":  "github.com/eshu-hq/supply-chain-demo-module/internal/vuln",
						"function": "Vulnerable",
					},
				},
			},
		},
		{
			// Standalone and schema-less (facts.VulnerabilityWarningFactKind is
			// registry-only per go/internal/ifa/catalog_seed.go); no case in
			// addSupplyChainImpactIndexEntry consumes it.
			FactID:   "debian-warning-scd-00010",
			FactKind: facts.VulnerabilityWarningFactKind,
			ScopeID:  scopeID,
			Payload: map[string]any{
				"code":    "advisory_source_unavailable",
				"message": "Synthetic advisory source unavailable during collection.",
				"source":  "debian",
			},
		},
	}
}
