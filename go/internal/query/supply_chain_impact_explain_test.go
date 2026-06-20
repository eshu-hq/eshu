package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type recordingSupplyChainImpactExplanationStore struct {
	row        SupplyChainImpactExplanationRow
	err        error
	lastFilter SupplyChainImpactExplanationFilter
}

func (s *recordingSupplyChainImpactExplanationStore) ExplainSupplyChainImpact(
	_ context.Context,
	filter SupplyChainImpactExplanationFilter,
) (SupplyChainImpactExplanationRow, error) {
	s.lastFilter = filter
	if s.err != nil {
		return SupplyChainImpactExplanationRow{}, s.err
	}
	return s.row, nil
}

func TestSupplyChainExplainImpactRequiresBoundedInput(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{ImpactExplanations: &recordingSupplyChainImpactExplanationStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/impact/explain",
		"/api/v0/supply-chain/impact/explain?advisory_id=GHSA-test",
		"/api/v0/supply-chain/impact/explain?package_id=pkg:npm/example",
		"/api/v0/supply-chain/impact/explain?advisory_id=GHSA-test&workload_id=workload:api",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestSupplyChainExplainImpactQueryUsesCanonicalFindingRows(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"canonical_key",
		"canonical_facts AS",
		"PARTITION BY canonical_key",
		"payload->>'finding_id'",
		"has_payload_finding_id",
	} {
		if !strings.Contains(explainSupplyChainImpactFindingQuery, want) {
			t.Fatalf("explainSupplyChainImpactFindingQuery missing canonical dedupe marker %q:\n%s", want, explainSupplyChainImpactFindingQuery)
		}
	}
}

func TestSupplyChainExplainImpactQueryKeepsRollingUpgradeFindingIDStable(t *testing.T) {
	t.Parallel()

	if strings.Contains(explainSupplyChainImpactFindingQuery, "COALESCE(NULLIF(fact.payload->>'finding_id', ''), fact.fact_id) AS finding_id") {
		t.Fatalf("explain query must not expose raw fact_id as legacy finding_id fallback:\n%s", explainSupplyChainImpactFindingQuery)
	}
	for _, want := range []string{
		"NULLIF(fact.payload->>'finding_id', '')",
		") AS finding_id",
		"ORDER BY priority_score DESC, has_payload_finding_id DESC, fact_id ASC",
	} {
		if !strings.Contains(explainSupplyChainImpactFindingQuery, want) {
			t.Fatalf("explain query missing rolling-upgrade canonical finding marker %q:\n%s", want, explainSupplyChainImpactFindingQuery)
		}
	}
}

func TestSupplyChainExplainImpactFindingIncludesEvidenceChain(t *testing.T) {
	t.Parallel()

	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 2, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilySBOMComponent, FactCount: 1, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyContainerImageIdentity, FactCount: 1, Freshness: FreshnessLabelFresh},
			},
		},
	}
	store := &recordingSupplyChainImpactExplanationStore{
		row: exactManifestAndImageExplanationRow(),
	}
	handler := &SupplyChainHandler{ImpactExplanations: store, Readiness: readiness}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/explain?finding_id=finding-1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.FindingID, "finding-1"; got != want {
		t.Fatalf("FindingID = %q, want %q", got, want)
	}

	var resp SupplyChainImpactExplanationResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.Outcome, "finding_explained"; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if got, want := resp.EvidencePacketHandle, "supply-chain-impact-explanation:finding:finding-1"; got != want {
		t.Fatalf("EvidencePacketHandle = %q, want %q", got, want)
	}
	if resp.Finding == nil || resp.Finding.FindingID != "finding-1" {
		t.Fatalf("Finding = %#v, want finding-1", resp.Finding)
	}
	if got, want := resp.Advisory.CVEID, "CVE-2026-0001"; got != want {
		t.Fatalf("Advisory.CVEID = %q, want %q", got, want)
	}
	if got, want := resp.Advisory.VulnerableRange, "<2.0.0"; got != want {
		t.Fatalf("Advisory.VulnerableRange = %q, want %q", got, want)
	}
	if got, want := resp.Component.ObservedVersion, "1.2.3"; got != want {
		t.Fatalf("Component.ObservedVersion = %q, want %q", got, want)
	}
	if got, want := resp.Version.FixedVersion, "2.0.0"; got != want {
		t.Fatalf("Version.FixedVersion = %q, want %q", got, want)
	}
	if resp.DependencyChain == nil {
		t.Fatal("DependencyChain = nil, want path")
	}
	if got, want := resp.DependencyChain.Path, []string{"api", "left-pad"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DependencyChain.Path = %#v, want %#v", got, want)
	}
	if got, want := resp.Anchors.ManifestPaths, []string{"package-lock.json"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Anchors.ManifestPaths = %#v, want %#v", got, want)
	}
	if got, want := resp.Anchors.SBOMDocuments, []string{"sbom-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Anchors.SBOMDocuments = %#v, want %#v", got, want)
	}
	if got, want := resp.Anchors.ImageDigests, []string{"sha256:abc"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Anchors.ImageDigests = %#v, want %#v", got, want)
	}
	if got, want := resp.Anchors.Workloads, []string{"workload:api"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Anchors.Workloads = %#v, want %#v", got, want)
	}
	if got, want := resp.Freshness.LatestObservedAt, "2026-05-24T12:00:00Z"; got != want {
		t.Fatalf("Freshness.LatestObservedAt = %q, want %q", got, want)
	}
}

func TestBuildSupplyChainImpactExplanationCoversEvidenceClasses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                string
		row                 SupplyChainImpactExplanationRow
		wantOutcome         string
		wantVersionEvidence string
		wantMissing         string
		wantProviderAlert   string
		wantImageDigest     string
	}{
		{
			name:                "exact finding explains direct evidence",
			row:                 exactManifestAndImageExplanationRow(),
			wantOutcome:         "finding_explained",
			wantVersionEvidence: "exact",
			wantImageDigest:     "sha256:abc",
		},
		{
			name: "range-only finding keeps observed version unknown",
			row: SupplyChainImpactExplanationRow{
				Finding: SupplyChainImpactFindingRow{
					FindingID:       "finding-range",
					CVEID:           "CVE-2026-0002",
					PackageID:       "pkg:npm/range-only",
					Ecosystem:       "npm",
					PackageName:     "range-only",
					ImpactStatus:    "possibly_affected",
					RepositoryID:    "repo://example/api",
					FixedVersion:    "3.0.0",
					DependencyPath:  []string{"range-only"},
					DependencyDepth: 1,
					MissingEvidence: []string{"observed_version"},
					EvidenceFactIDs: []string{"affected-range", "consume-range"},
				},
				EvidenceFacts: []SupplyChainImpactEvidenceFact{
					explanationFact("affected-range", "vulnerability.affected_package", map[string]any{
						"cve_id":         "CVE-2026-0002",
						"package_id":     "pkg:npm/range-only",
						"affected_range": ">=1.0.0 <3.0.0",
						"fixed_versions": []any{"3.0.0"},
						"source":         "osv",
					}),
					explanationFact("consume-range", "reducer_package_consumption_correlation", map[string]any{
						"repository_id":    "repo://example/api",
						"relative_path":    "package.json",
						"dependency_range": "^2.0.0",
					}),
				},
			},
			wantOutcome:         "finding_explained",
			wantVersionEvidence: "range_only",
			wantMissing:         "observed_version",
		},
		{
			name: "provider-only alert stays missing owned evidence",
			row: SupplyChainImpactExplanationRow{
				Finding: SupplyChainImpactFindingRow{
					FindingID:       "finding-provider",
					CVEID:           "CVE-2026-0003",
					AdvisoryID:      "GHSA-provider",
					PackageID:       "pkg:npm/provider-only",
					PackageName:     "provider-only",
					ImpactStatus:    "unknown_impact",
					RepositoryID:    "repo://example/api",
					MissingEvidence: []string{"owned_packages", "advisory_sources"},
					EvidenceFactIDs: []string{"provider-alert"},
				},
				EvidenceFacts: []SupplyChainImpactEvidenceFact{
					explanationFact("provider-alert", "provider.security_alert", map[string]any{
						"provider":      "github",
						"alert_id":      "alert-7",
						"state":         "open",
						"manifest_path": "package.json",
					}),
				},
			},
			wantOutcome:       "finding_explained",
			wantMissing:       "owned_packages",
			wantProviderAlert: "alert-7",
		},
		{
			name: "sbom image finding exposes image anchor",
			row: SupplyChainImpactExplanationRow{
				Finding: SupplyChainImpactFindingRow{
					FindingID:           "finding-image",
					CVEID:               "CVE-2026-0004",
					PackageID:           "pkg:npm/image-only",
					Ecosystem:           "npm",
					PackageName:         "image-only",
					ObservedVersion:     "4.5.6",
					FixedVersion:        "4.5.7",
					ImpactStatus:        "affected_derived",
					RuntimeReachability: "image_sbom",
					SubjectDigest:       "sha256:def",
					EvidenceFactIDs:     []string{"component-image", "attachment-image", "image-identity"},
				},
				EvidenceFacts: []SupplyChainImpactEvidenceFact{
					explanationFact("component-image", "sbom.component", map[string]any{
						"document_id": "sbom-image",
						"purl":        "pkg:npm/image-only@4.5.6",
						"version":     "4.5.6",
					}),
					explanationFact("attachment-image", "reducer_sbom_attestation_attachment", map[string]any{
						"document_id":       "sbom-image",
						"subject_digest":    "sha256:def",
						"artifact_kind":     "sbom",
						"attachment_status": "attached_verified",
					}),
					explanationFact("image-identity", "reducer_container_image_identity", map[string]any{
						"digest":        "sha256:def",
						"repository_id": "oci-registry://registry.example/api",
					}),
				},
			},
			wantOutcome:         "finding_explained",
			wantVersionEvidence: "exact",
			wantImageDigest:     "sha256:def",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := BuildSupplyChainImpactExplanation(
				SupplyChainImpactExplanationFilter{FindingID: tc.row.Finding.FindingID},
				tc.row,
				SupplyChainImpactReadinessEnvelope{State: ReadinessStateReadyWithFindings},
			)
			if got.Outcome != tc.wantOutcome {
				t.Fatalf("Outcome = %q, want %q", got.Outcome, tc.wantOutcome)
			}
			if tc.wantVersionEvidence != "" && got.Version.VersionEvidence != tc.wantVersionEvidence {
				t.Fatalf("Version.VersionEvidence = %q, want %q", got.Version.VersionEvidence, tc.wantVersionEvidence)
			}
			if tc.wantMissing != "" && !containsString(got.MissingEvidence, tc.wantMissing) {
				t.Fatalf("MissingEvidence = %#v, want %q", got.MissingEvidence, tc.wantMissing)
			}
			if tc.wantProviderAlert != "" {
				if len(got.Anchors.ProviderAlerts) != 1 || got.Anchors.ProviderAlerts[0].AlertID != tc.wantProviderAlert {
					t.Fatalf("ProviderAlerts = %#v, want alert %q", got.Anchors.ProviderAlerts, tc.wantProviderAlert)
				}
			}
			if tc.wantImageDigest != "" && !containsString(got.Anchors.ImageDigests, tc.wantImageDigest) {
				t.Fatalf("ImageDigests = %#v, want %q", got.Anchors.ImageDigests, tc.wantImageDigest)
			}
		})
	}
}

func TestSupplyChainExplainImpactNoEvidenceResponse(t *testing.T) {
	t.Parallel()

	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 1, Freshness: FreshnessLabelFresh},
			},
		},
	}
	store := &recordingSupplyChainImpactExplanationStore{
		err: ErrSupplyChainImpactExplanationNotFound,
	}
	handler := &SupplyChainHandler{ImpactExplanations: store, Readiness: readiness}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/explain?advisory_id=GHSA-missing&repository_id=repo://example/api",
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
	if got, want := resp.Outcome, "no_finding"; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if got, want := resp.Advisory.AdvisoryID, "GHSA-missing"; got != want {
		t.Fatalf("Advisory.AdvisoryID = %q, want %q", got, want)
	}
	if got, want := resp.Version.VersionEvidence, "missing"; got != want {
		t.Fatalf("Version.VersionEvidence = %q, want %q", got, want)
	}
	if resp.Evidence == nil {
		t.Fatal("Evidence = nil, want empty slice")
	}
	if resp.Finding != nil {
		t.Fatalf("Finding = %#v, want nil", resp.Finding)
	}
	if !containsString(resp.MissingEvidence, "impact_finding") {
		t.Fatalf("MissingEvidence = %#v, want impact_finding", resp.MissingEvidence)
	}
	if !containsString(resp.MissingEvidence, MissingEvidenceOwnedPackages) {
		t.Fatalf("MissingEvidence = %#v, want %q", resp.MissingEvidence, MissingEvidenceOwnedPackages)
	}
}

func exactManifestAndImageExplanationRow() SupplyChainImpactExplanationRow {
	return SupplyChainImpactExplanationRow{
		Finding: SupplyChainImpactFindingRow{
			FindingID:           "finding-1",
			CVEID:               "CVE-2026-0001",
			AdvisoryID:          "GHSA-test-1",
			PackageID:           "pkg:npm/left-pad",
			Ecosystem:           "npm",
			PackageName:         "left-pad",
			PURL:                "pkg:npm/left-pad@1.2.3",
			ObservedVersion:     "1.2.3",
			FixedVersion:        "2.0.0",
			ImpactStatus:        "affected_exact",
			Confidence:          "exact",
			RuntimeReachability: "package_manifest",
			RepositoryID:        "repo://example/api",
			SubjectDigest:       "sha256:abc",
			DependencyPath:      []string{"api", "left-pad"},
			DependencyDepth:     2,
			DirectDependency:    boolPtr(false),
			EvidencePath:        []string{"vulnerability.affected_package", "reducer_package_consumption_correlation", "sbom.component"},
			EvidenceFactIDs:     []string{"affected-1", "consume-1", "component-1", "attach-1", "image-1", "workload-1"},
			Provenance: &SupplyChainImpactProvenance{
				SelectedRangeSource:        "ghsa",
				SelectedFixedVersionSource: "ghsa",
				AdvisorySources: []SupplyChainAdvisorySource{
					{Source: "ghsa", AdvisoryID: "GHSA-test-1", SourceUpdatedAt: "2026-05-24T11:00:00Z"},
				},
			},
		},
		EvidenceFacts: []SupplyChainImpactEvidenceFact{
			explanationFact("affected-1", "vulnerability.affected_package", map[string]any{
				"cve_id":         "CVE-2026-0001",
				"advisory_id":    "GHSA-test-1",
				"package_id":     "pkg:npm/left-pad",
				"package_name":   "left-pad",
				"affected_range": "<2.0.0",
				"fixed_versions": []any{"2.0.0"},
				"source":         "ghsa",
				"references":     []any{"https://github.com/advisories/GHSA-test-1"},
			}),
			explanationFact("consume-1", "reducer_package_consumption_correlation", map[string]any{
				"repository_id":     "repo://example/api",
				"relative_path":     "package-lock.json",
				"manifest_section":  "dependencies",
				"dependency_range":  "1.2.3",
				"dependency_path":   []any{"api", "left-pad"},
				"dependency_depth":  float64(2),
				"direct_dependency": false,
			}),
			explanationFact("component-1", "sbom.component", map[string]any{
				"document_id": "sbom-1",
				"purl":        "pkg:npm/left-pad@1.2.3",
				"version":     "1.2.3",
			}),
			explanationFact("attach-1", "reducer_sbom_attestation_attachment", map[string]any{
				"document_id":         "sbom-1",
				"subject_digest":      "sha256:abc",
				"artifact_kind":       "sbom",
				"attachment_status":   "attached_verified",
				"verification_status": "passed",
			}),
			explanationFact("image-1", "reducer_container_image_identity", map[string]any{
				"digest":        "sha256:abc",
				"image_ref":     "registry.example/api@sha256:abc",
				"repository_id": "repo://example/api",
			}),
			explanationFact("workload-1", "reducer_workload_identity", map[string]any{
				"workload_id": "workload:api",
				"environment": "prod",
			}),
		},
	}
}

func explanationFact(factID, factKind string, payload map[string]any) SupplyChainImpactEvidenceFact {
	return SupplyChainImpactEvidenceFact{
		FactID:           factID,
		FactKind:         factKind,
		SourceSystem:     "test",
		SourceConfidence: "inferred",
		ObservedAt:       time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		Payload:          payload,
	}
}

func TestSupplyChainExplainImpactStoreErrorSentinelIdentity(t *testing.T) {
	t.Parallel()

	if !errors.Is(fmt.Errorf("wrap: %w", ErrSupplyChainImpactExplanationNotFound), ErrSupplyChainImpactExplanationNotFound) {
		t.Fatal("ErrSupplyChainImpactExplanationNotFound must support errors.Is")
	}
	if !errors.Is(fmt.Errorf("wrap: %w", ErrSupplyChainImpactExplanationAmbiguous), ErrSupplyChainImpactExplanationAmbiguous) {
		t.Fatal("ErrSupplyChainImpactExplanationAmbiguous must support errors.Is")
	}
}
