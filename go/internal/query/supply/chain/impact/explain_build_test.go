// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"errors"
	"testing"
	"time"
)

func explainBuildTestRow() ExplanationRow {
	return ExplanationRow{
		Finding: FindingRow{
			FindingID:       "finding-1",
			CVEID:           "CVE-2026-0001",
			AdvisoryID:      "GHSA-aaaa-bbbb-cccc",
			PackageID:       "pkg:npm/left-pad@1.3.0",
			Ecosystem:       "npm",
			PackageName:     "left-pad",
			ObservedVersion: "1.3.0",
			FixedVersion:    "1.3.1",
			RepositoryID:    "repo-1",
			WorkloadIDs:     []string{"workload-1"},
			Provenance: &Provenance{
				SelectedRangeSource:        "advisory",
				SelectedSeveritySource:     "advisory",
				SelectedFixedVersionSource: "advisory",
			},
		},
		EvidenceFacts: []EvidenceFact{
			{
				FactID:   "fact-1",
				FactKind: "vulnerability.advisory",
				Payload: map[string]any{
					"affected_range": ">= 1.0.0, < 1.3.1",
					"references":     []any{"https://example.com/advisory"},
					"url":            "https://example.com/details",
				},
			},
		},
	}
}

// TestBuildSupplyChainAdvisoryExplanation proves the advisory builder
// carries the finding identity, the provenance sources, and the
// vulnerability-range evidence with deduplicated references.
func TestBuildSupplyChainAdvisoryExplanation(t *testing.T) {
	t.Parallel()

	advisory := buildSupplyChainAdvisoryExplanation(explainBuildTestRow())
	if advisory.CVEID != "CVE-2026-0001" {
		t.Errorf("CVEID = %q", advisory.CVEID)
	}
	if advisory.VulnerableRange != ">= 1.0.0, < 1.3.1" {
		t.Errorf("VulnerableRange = %q", advisory.VulnerableRange)
	}
	if advisory.RangeSource != "advisory" {
		t.Errorf("RangeSource = %q, want advisory", advisory.RangeSource)
	}
	if len(advisory.References) != 2 {
		t.Errorf("References = %v, want the two deduplicated URLs", advisory.References)
	}
}

// TestBuildSupplyChainComponentExplanation proves the component builder
// reads manifest evidence off unmatched reducer-derived facts raw.
func TestBuildSupplyChainComponentExplanation(t *testing.T) {
	t.Parallel()

	row := explainBuildTestRow()
	row.EvidenceFacts = append(row.EvidenceFacts, EvidenceFact{
		FactID:   "fact-2",
		FactKind: "reducer_package_consumption_correlation",
		Payload: map[string]any{
			"manifest_range": ">= 1.0.0",
			"version":        "1.3.0",
			"purl":           "pkg:npm/left-pad@1.3.0",
		},
	})
	component := buildSupplyChainComponentExplanation(row)
	if component.ManifestRange != ">= 1.0.0" {
		t.Errorf("ManifestRange = %q", component.ManifestRange)
	}
	if component.PackageName != "left-pad" || component.Ecosystem != "npm" {
		t.Errorf("component identity lost: %+v", component)
	}
}

// TestBuildSupplyChainVersionExplanation proves the version evidence
// ladder: exact beats range-only beats missing, with fixed versions
// falling back to fact payloads.
func TestBuildSupplyChainVersionExplanation(t *testing.T) {
	t.Parallel()

	row := explainBuildTestRow()
	advisory := buildSupplyChainAdvisoryExplanation(row)
	component := buildSupplyChainComponentExplanation(row)
	version := buildSupplyChainVersionExplanation(row, advisory, component)
	if version.VersionEvidence != "exact" {
		t.Errorf("VersionEvidence = %q, want exact", version.VersionEvidence)
	}
	if version.FixedVersion != "1.3.1" {
		t.Errorf("FixedVersion = %q", version.FixedVersion)
	}

	ranged := ComponentExplanation{ManifestRange: ">= 1.0.0"}
	if got := buildSupplyChainVersionExplanation(row, advisory, ranged); got.VersionEvidence != "range_only" {
		t.Errorf("ranged VersionEvidence = %q, want range_only", got.VersionEvidence)
	}

	unfixed := explainBuildTestRow()
	unfixed.Finding.FixedVersion = ""
	unfixed.EvidenceFacts = append(unfixed.EvidenceFacts, EvidenceFact{
		FactID:   "fact-9",
		FactKind: "vulnerability.advisory",
		Payload:  map[string]any{"fixed_versions": []any{"2.0.0"}},
	})
	if got := buildSupplyChainVersionExplanation(unfixed, advisory, component); got.FixedVersion != "2.0.0" {
		t.Errorf("fallback FixedVersion = %q, want 2.0.0", got.FixedVersion)
	}
}

// TestBuildSupplyChainDependencyChain proves the chain builder clones
// the finding path and returns nil when no chain evidence exists.
func TestBuildSupplyChainDependencyChain(t *testing.T) {
	t.Parallel()

	direct := true
	row := explainBuildTestRow()
	row.Finding.DependencyPath = []string{"root", "left-pad"}
	row.Finding.DependencyDepth = 2
	row.Finding.DirectDependency = &direct
	chain := buildSupplyChainDependencyChain(row.Finding, row.EvidenceFacts)
	if chain == nil {
		t.Fatal("finding-anchored chain must not be nil")
	}
	if len(chain.Path) != 2 || chain.Depth != 2 || chain.DirectDependency == nil || !*chain.DirectDependency {
		t.Errorf("chain mismatch: %+v", chain)
	}
	if buildSupplyChainDependencyChain(FindingRow{}, nil) != nil {
		t.Error("evidenceless chain must be nil")
	}
}

// TestBuildSupplyChainExplanationAnchors proves anchor collection
// merges finding fields with raw fact payload anchors.
func TestBuildSupplyChainExplanationAnchors(t *testing.T) {
	t.Parallel()

	row := explainBuildTestRow()
	row.EvidenceFacts = append(row.EvidenceFacts, EvidenceFact{
		FactID:   "fact-3",
		FactKind: "reducer_container_image_identity",
		Payload: map[string]any{
			"manifest_path": "package-lock.json",
			"image_ref":     "registry/app:1.0",
			"digest":        "sha256:abc",
		},
	})
	anchors := buildSupplyChainExplanationAnchors(row)
	if anchors.RepositoryID != "repo-1" {
		t.Errorf("RepositoryID = %q", anchors.RepositoryID)
	}
	if len(anchors.ManifestPaths) != 1 || anchors.ManifestPaths[0] != "package-lock.json" {
		t.Errorf("ManifestPaths = %v", anchors.ManifestPaths)
	}
	if len(anchors.LockfilePaths) != 1 {
		t.Errorf("package-lock.json must also anchor as a lockfile: %v", anchors.LockfilePaths)
	}
	if len(anchors.Workloads) != 1 || anchors.Workloads[0] != "workload-1" {
		t.Errorf("Workloads = %v", anchors.Workloads)
	}
	if len(anchors.EvidenceFactIDs) != 2 {
		t.Errorf("EvidenceFactIDs = %v, want both facts", anchors.EvidenceFactIDs)
	}
}

// TestExplanationMissingEvidence proves the missing-evidence list
// unions readiness gaps with absent advisory, version, fix, and chain
// signals.
func TestExplanationMissingEvidence(t *testing.T) {
	t.Parallel()

	row := FindingRow{}
	readiness := ReadinessEnvelope{MissingEvidence: []string{"owned_packages"}}
	missing := explanationMissingEvidence(
		row, readiness,
		AdvisoryExplanation{}, ComponentExplanation{},
		VersionExplanation{VersionEvidence: "missing"}, nil, ExplanationAnchors{},
	)
	for _, want := range []string{"owned_packages", "vulnerable_range", "observed_version", "fixed_version", "dependency_chain"} {
		found := false
		for _, reason := range missing {
			if reason == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing evidence %v lacks %q", missing, want)
		}
	}
}

// TestSummarizeSupplyChainEvidenceFacts proves fact summaries carry
// identity with RFC3339 observations and skip zero times.
func TestSummarizeSupplyChainEvidenceFacts(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	summaries := summarizeSupplyChainEvidenceFacts([]EvidenceFact{
		{FactID: "f1", FactKind: "k", SourceSystem: "s", ObservedAt: observed},
		{FactID: "f2", FactKind: "k"},
	})
	if len(summaries) != 2 {
		t.Fatalf("len(summaries) = %d, want 2", len(summaries))
	}
	if summaries[0].ObservedAt != "2026-10-01T12:00:00Z" {
		t.Errorf("ObservedAt = %q", summaries[0].ObservedAt)
	}
	if summaries[1].ObservedAt != "" {
		t.Errorf("zero time must stay empty, got %q", summaries[1].ObservedAt)
	}
}

// TestSupplyChainExplanationFreshness proves the freshness envelope
// counts facts, reports the latest observation, and defaults state.
func TestSupplyChainExplanationFreshness(t *testing.T) {
	t.Parallel()

	older := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	freshness := supplyChainExplanationFreshness([]EvidenceFact{
		{FactID: "f1", ObservedAt: newer},
		{FactID: "f2", ObservedAt: older},
	}, "")
	if freshness.EvidenceFactCount != 2 {
		t.Errorf("EvidenceFactCount = %d, want 2", freshness.EvidenceFactCount)
	}
	if freshness.LatestObservedAt != "2026-10-01T00:00:00Z" {
		t.Errorf("LatestObservedAt = %q", freshness.LatestObservedAt)
	}
	if freshness.State != FreshnessLabelUnknown {
		t.Errorf("State = %q, want the unknown label", freshness.State)
	}
	if got := supplyChainExplanationFreshness(nil, "fresh"); got.State != "fresh" {
		t.Errorf("explicit readiness freshness must pass through, got %q", got.State)
	}
}

// TestProviderAlertAnchor proves alert facts anchor with their
// provider identity while non-alert facts without alert ids anchor
// to nothing.
func TestProviderAlertAnchor(t *testing.T) {
	t.Parallel()

	alert := providerAlertAnchor(EvidenceFact{
		FactKind: "security.alert",
		Payload:  map[string]any{"provider": "github", "alert_id": "1", "state": "open", "manifest_path": "go.mod"},
	})
	if alert.Provider != "github" || alert.AlertID != "1" || alert.ManifestPath != "go.mod" {
		t.Errorf("alert anchor mismatch: %+v", alert)
	}
	if got := providerAlertAnchor(EvidenceFact{FactKind: "sbom.component", Payload: map[string]any{}}); got != (ProviderAlertAnchor{}) {
		t.Errorf("non-alert fact must anchor to nothing: %+v", got)
	}
}

// TestBuildSupplyChainRemediationExplanation proves the remediation
// builder fills display values from the explain outputs without
// overwriting persisted fields, and derives the parent package.
func TestBuildSupplyChainRemediationExplanation(t *testing.T) {
	t.Parallel()

	direct := true
	row := explainBuildTestRow()
	row.Finding.Remediation = &Remediation{Reason: "upgrade", Confidence: "high"}
	chain := &DependencyChain{Path: []string{"root", "left-pad"}, DirectDependency: &direct}
	remediation := buildSupplyChainRemediationExplanation(
		row,
		AdvisoryExplanation{VulnerableRange: ">= 1.0.0"},
		VersionExplanation{FixedVersion: "1.3.1"},
		ComponentExplanation{ManifestRange: ">= 1.0.0", ObservedVersion: "1.3.0", Ecosystem: "npm"},
		chain,
	)
	if remediation == nil {
		t.Fatal("remediation must not be nil when the finding carries a block")
	}
	if remediation.VulnerableRange != ">= 1.0.0" || remediation.FirstPatchedVersion != "1.3.1" {
		t.Errorf("remediation fill-in missed: %+v", remediation)
	}
	if remediation.Reason != "upgrade" {
		t.Errorf("persisted reason must survive: %+v", remediation)
	}
	if remediation.Direct == nil || !*remediation.Direct {
		t.Errorf("direct flag must derive from the chain: %+v", remediation)
	}
	if remediation.ParentPackage != "root" {
		t.Errorf("ParentPackage = %q, want root", remediation.ParentPackage)
	}
	if buildSupplyChainRemediationExplanation(ExplanationRow{}, AdvisoryExplanation{}, VersionExplanation{}, ComponentExplanation{}, nil) != nil {
		t.Error("remediation-less findings must yield nil")
	}
	if remediationParentFromChain([]string{"only"}) != "" {
		t.Error("single-hop chains have no parent package")
	}
}

// TestExplanationAmbiguousCandidateCount proves the ambiguous-scope
// error carries its candidate count with a floor of two.
func TestExplanationAmbiguousCandidateCount(t *testing.T) {
	t.Parallel()

	if got := ExplanationAmbiguousCandidateCount(newSupplyChainImpactExplanationAmbiguousError(5)); got != 5 {
		t.Errorf("candidate count = %d, want 5", got)
	}
	if got := ExplanationAmbiguousCandidateCount(newSupplyChainImpactExplanationAmbiguousError(0)); got != 2 {
		t.Errorf("floored candidate count = %d, want 2", got)
	}
	if got := ExplanationAmbiguousCandidateCount(ErrExplanationAmbiguous); got != 2 {
		t.Errorf("bare sentinel count = %d, want 2", got)
	}
	if got := ExplanationAmbiguousCandidateCount(errors.New("nope")); got != 0 {
		t.Errorf("foreign error count = %d, want 0", got)
	}
}

// TestTrimExplanationFilter proves the filter trimmer strips padding
// on every selector field.
func TestTrimExplanationFilter(t *testing.T) {
	t.Parallel()

	trimmed := TrimExplanationFilter(ExplanationFilter{
		FindingID: " f1 ", CVEID: " CVE-1 ", PackageID: " p ",
		RepositoryID: " r ", SubjectDigest: " s ", ImageRef: " i ",
		WorkloadID: " w ", ServiceID: " svc ", AdvisoryID: " a ",
	})
	if trimmed.FindingID != "f1" || trimmed.CVEID != "CVE-1" || trimmed.ServiceID != "svc" {
		t.Errorf("filter not fully trimmed: %+v", trimmed)
	}
}
