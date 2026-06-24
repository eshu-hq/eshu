// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSupplyChainExplainImpactSurfacesRemediation proves that the impact
// explain HTTP route forwards the reducer-owned remediation block to the
// public response so MCP/API callers can read confidence, reason, manifest
// allowance, parent package, and first patched version without re-querying
// the finding list.
func TestSupplyChainExplainImpactSurfacesRemediation(t *testing.T) {
	t.Parallel()

	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 1, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: FreshnessLabelFresh},
			},
		},
	}
	row := remediationExplanationRow()
	store := &recordingSupplyChainImpactExplanationStore{row: row}
	handler := &SupplyChainHandler{ImpactExplanations: store, Readiness: readiness}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/explain?finding_id=finding-remediation",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp SupplyChainImpactExplanationResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Remediation == nil {
		t.Fatalf("Remediation = nil, want populated remediation block")
	}
	if got, want := resp.Remediation.Reason, "direct_upgrade_allowed"; got != want {
		t.Fatalf("Remediation.Reason = %q, want %q", got, want)
	}
	if got, want := resp.Remediation.Confidence, "exact"; got != want {
		t.Fatalf("Remediation.Confidence = %q, want %q", got, want)
	}
	if got, want := resp.Remediation.FirstPatchedVersion, "1.3.0"; got != want {
		t.Fatalf("Remediation.FirstPatchedVersion = %q, want %q", got, want)
	}
	if got, want := resp.Remediation.ManifestAllowsFix, "allowed"; got != want {
		t.Fatalf("Remediation.ManifestAllowsFix = %q, want %q", got, want)
	}
	if resp.Remediation.VulnerableRange == "" {
		t.Fatal("Remediation.VulnerableRange = empty, want enrichment from advisory facts")
	}
}

// TestSupplyChainExplainImpactRemediationEnrichesTransitiveParent proves that
// the explain build path fills in the parent package from the dependency
// chain when the persisted remediation block did not carry one (older
// rows or chains without an exact parent).
func TestSupplyChainExplainImpactRemediationEnrichesTransitiveParent(t *testing.T) {
	t.Parallel()

	row := remediationExplanationRow()
	row.Finding.Remediation = &SupplyChainImpactRemediation{
		Reason:              "transitive_parent_upgrade_required",
		Confidence:          "partial",
		FirstPatchedVersion: "2.3.4",
		ManifestAllowsFix:   "unknown",
	}
	row.Finding.DependencyPath = []string{"vite", "rollup", "fsevents"}
	row.Finding.DependencyDepth = 3
	direct := false
	row.Finding.DirectDependency = &direct

	store := &recordingSupplyChainImpactExplanationStore{row: row}
	handler := &SupplyChainHandler{
		ImpactExplanations: store,
		Readiness: &recordingSupplyChainImpactReadinessStore{
			snapshot: SupplyChainImpactReadinessSnapshot{},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/explain?finding_id=finding-remediation",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp SupplyChainImpactExplanationResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Remediation == nil {
		t.Fatalf("Remediation = nil, want populated remediation block")
	}
	if got, want := resp.Remediation.ParentPackage, "rollup"; got != want {
		t.Fatalf("Remediation.ParentPackage = %q, want %q", got, want)
	}
	if resp.Remediation.Direct == nil || *resp.Remediation.Direct {
		t.Fatalf("Remediation.Direct = %#v, want false enriched from dependency chain", resp.Remediation.Direct)
	}
}

func TestDecodeSupplyChainImpactRemediationPreservesMatchAndSourceTruth(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"remediation": map[string]any{
			"ecosystem":                "maven",
			"current_version":          "3.9.8",
			"vulnerable_range":         "[3.8.0,3.9.9)",
			"first_patched_version":    "3.9.9",
			"fixed_version_source":     "ghsa",
			"match_reason":             "maven_range_match",
			"manifest_range":           "[3.8.0,4.0.0)",
			"manifest_allows_fix":      "allowed",
			"confidence":               "exact",
			"reason":                   "direct_upgrade_allowed",
			"patched_version_branches": []any{map[string]any{"version": "3.9.9", "source": "ghsa"}},
		},
	}

	remediation := decodeSupplyChainImpactRemediation(payload)
	if remediation == nil {
		t.Fatal("decodeSupplyChainImpactRemediation() = nil, want remediation")
	}
	if remediation.MatchReason != "maven_range_match" {
		t.Fatalf("MatchReason = %q, want maven_range_match", remediation.MatchReason)
	}
	if remediation.FixedVersionSource != "ghsa" {
		t.Fatalf("FixedVersionSource = %q, want ghsa", remediation.FixedVersionSource)
	}
}

func remediationExplanationRow() SupplyChainImpactExplanationRow {
	direct := true
	return SupplyChainImpactExplanationRow{
		Finding: SupplyChainImpactFindingRow{
			FindingID:        "finding-remediation",
			CVEID:            "CVE-2026-90099",
			AdvisoryID:       "GHSA-rem-1",
			PackageID:        "pkg:npm/example",
			Ecosystem:        "npm",
			PackageName:      "example",
			ObservedVersion:  "1.2.3",
			RequestedRange:   "^1.2.0",
			FixedVersion:     "1.3.0",
			ImpactStatus:     "affected_exact",
			Confidence:       "exact",
			RepositoryID:     "repo://example/api",
			DependencyPath:   []string{"example"},
			DependencyDepth:  1,
			DirectDependency: &direct,
			Remediation: &SupplyChainImpactRemediation{
				Ecosystem:           "npm",
				CurrentVersion:      "1.2.3",
				FirstPatchedVersion: "1.3.0",
				ManifestRange:       "^1.2.0",
				ManifestAllowsFix:   "allowed",
				Direct:              boolPtr(true),
				Confidence:          "exact",
				Reason:              "direct_upgrade_allowed",
				PatchedVersionBranches: []SupplyChainFixedVersionBranch{
					{Version: "1.3.0", Source: "ghsa"},
				},
			},
			EvidenceFactIDs: []string{"affected-rem", "consume-rem"},
		},
		EvidenceFacts: []SupplyChainImpactEvidenceFact{
			explanationFact("affected-rem", "vulnerability.affected_package", map[string]any{
				"cve_id":         "CVE-2026-90099",
				"advisory_id":    "GHSA-rem-1",
				"package_id":     "pkg:npm/example",
				"affected_range": "<1.3.0",
				"fixed_versions": []any{"1.3.0"},
				"source":         "ghsa",
			}),
			explanationFact("consume-rem", "reducer_package_consumption_correlation", map[string]any{
				"repository_id":     "repo://example/api",
				"relative_path":     "package-lock.json",
				"dependency_range":  "^1.2.0",
				"dependency_path":   []any{"example"},
				"dependency_depth":  float64(1),
				"direct_dependency": true,
			}),
		},
	}
}
