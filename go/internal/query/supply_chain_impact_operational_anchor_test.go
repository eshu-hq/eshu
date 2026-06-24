// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestSupplyChainListImpactFindingsExposesOperationalAnchors(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{operationalAnchorFindingRow()},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Findings []SupplyChainImpactFindingResult `json:"findings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, want := len(resp.Findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d", got, want)
	}
	got := resp.Findings[0]
	if !reflect.DeepEqual(got.WorkloadIDs, []string{"workload:example-api"}) {
		t.Fatalf("WorkloadIDs = %#v, want workload anchor", got.WorkloadIDs)
	}
	if !reflect.DeepEqual(got.ServiceIDs, []string{"service:example-api"}) {
		t.Fatalf("ServiceIDs = %#v, want service anchor", got.ServiceIDs)
	}
	if !reflect.DeepEqual(got.Environments, []string{"prod"}) {
		t.Fatalf("Environments = %#v, want environment anchor", got.Environments)
	}
	if !reflect.DeepEqual(got.CatalogEntityRefs, []string{"api:default/example-api"}) {
		t.Fatalf("CatalogEntityRefs = %#v, want catalog entity anchor", got.CatalogEntityRefs)
	}
	if !reflect.DeepEqual(got.CatalogOwnerRefs, []string{"team:default/platform"}) {
		t.Fatalf("CatalogOwnerRefs = %#v, want catalog owner anchor", got.CatalogOwnerRefs)
	}
	for _, reason := range []string{
		"environment evidence missing",
		"service catalog correlation evidence missing",
		"service evidence missing",
	} {
		if containsString(got.MissingEvidence, reason) {
			t.Fatalf("MissingEvidence = %#v, must not include %q with attached anchors", got.MissingEvidence, reason)
		}
	}
}

func TestSupplyChainListImpactFindingsSuppressesStaleCatalogAnchorMissing(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{catalogEntityOperationalFindingRow()},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Findings []SupplyChainImpactFindingResult `json:"findings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, want := len(resp.Findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d", got, want)
	}
	got := resp.Findings[0]
	if !reflect.DeepEqual(got.WorkloadIDs, []string{"workload:example-api"}) {
		t.Fatalf("WorkloadIDs = %#v, want workload anchor", got.WorkloadIDs)
	}
	if !reflect.DeepEqual(got.CatalogEntityRefs, []string{"api:default/example-api"}) {
		t.Fatalf("CatalogEntityRefs = %#v, want catalog entity anchor", got.CatalogEntityRefs)
	}
	if len(got.ServiceIDs) != 0 {
		t.Fatalf("ServiceIDs = %#v, want no fabricated service identity", got.ServiceIDs)
	}
	if containsString(got.MissingEvidence, serviceCatalogAnchorMissingReason) {
		t.Fatalf("MissingEvidence = %#v, must not claim catalog anchor is missing", got.MissingEvidence)
	}
	if !containsString(got.MissingEvidence, "environment evidence missing") {
		t.Fatalf("MissingEvidence = %#v, want remaining environment gap", got.MissingEvidence)
	}
}

func TestSupplyChainExplainImpactExposesOperationalAnchors(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactExplanationStore{
		row: SupplyChainImpactExplanationRow{
			Finding: operationalAnchorFindingRow(),
			EvidenceFacts: []SupplyChainImpactEvidenceFact{
				explanationFact("catalog-1", serviceCatalogCorrelationFactKind, map[string]any{
					"repository_id": "repo://example/api",
					"service_id":    "service:example-api",
					"workload_id":   "workload:example-api",
					"entity_ref":    "api:default/example-api",
					"owner_ref":     "team:default/platform",
					"outcome":       "exact",
				}),
				explanationFact("deploy-1", "reducer_ci_cd_run_correlation", map[string]any{
					"repository_id": "repo://example/api",
					"environment":   "prod",
					"outcome":       "exact",
				}),
			},
		},
	}
	handler := &SupplyChainHandler{ImpactExplanations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/explain?finding_id=finding-operational",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp SupplyChainImpactExplanationResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if resp.Finding == nil {
		t.Fatal("Finding = nil, want operational finding")
	}
	if !reflect.DeepEqual(resp.Anchors.Services, []string{"service:example-api"}) {
		t.Fatalf("Anchors.Services = %#v, want service anchor", resp.Anchors.Services)
	}
	if !reflect.DeepEqual(resp.Anchors.Environments, []string{"prod"}) {
		t.Fatalf("Anchors.Environments = %#v, want environment anchor", resp.Anchors.Environments)
	}
	if !reflect.DeepEqual(resp.Anchors.CatalogEntities, []string{"api:default/example-api"}) {
		t.Fatalf("Anchors.CatalogEntities = %#v, want catalog entity anchor", resp.Anchors.CatalogEntities)
	}
	if !reflect.DeepEqual(resp.Anchors.CatalogOwners, []string{"team:default/platform"}) {
		t.Fatalf("Anchors.CatalogOwners = %#v, want catalog owner anchor", resp.Anchors.CatalogOwners)
	}
	assertImpactPathHopStatus(t, resp.ImpactPath, "service", "present")
	assertImpactPathHopStatus(t, resp.ImpactPath, "environment", "present")
}

func TestSupplyChainExplainImpactTreatsCatalogEntityAsServiceHop(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactExplanationStore{
		row: SupplyChainImpactExplanationRow{
			Finding: catalogEntityOperationalFindingRow(),
			EvidenceFacts: []SupplyChainImpactEvidenceFact{
				explanationFact("catalog-1", serviceCatalogCorrelationFactKind, map[string]any{
					"repository_id": "repo://example/api",
					"entity_ref":    "api:default/example-api",
					"owner_ref":     "team:default/platform",
					"outcome":       "exact",
				}),
				explanationFact("workload-1", "reducer_workload_identity", map[string]any{
					"scope_id":    "repo://example/api",
					"entity_keys": []any{"workload:example-api"},
				}),
			},
		},
	}
	handler := &SupplyChainHandler{ImpactExplanations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/explain?finding_id=finding-catalog-entity",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp SupplyChainImpactExplanationResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if resp.Finding == nil {
		t.Fatal("Finding = nil, want catalog-entity finding")
	}
	if containsString(resp.Finding.MissingEvidence, serviceCatalogAnchorMissingReason) {
		t.Fatalf("Finding.MissingEvidence = %#v, must not claim catalog anchor is missing", resp.Finding.MissingEvidence)
	}
	if !reflect.DeepEqual(resp.Anchors.CatalogEntities, []string{"api:default/example-api"}) {
		t.Fatalf("Anchors.CatalogEntities = %#v, want catalog entity anchor", resp.Anchors.CatalogEntities)
	}
	if len(resp.Anchors.Services) != 0 {
		t.Fatalf("Anchors.Services = %#v, want no fabricated service identity", resp.Anchors.Services)
	}
	assertImpactPathHopStatus(t, resp.ImpactPath, "workload", "present")
	assertImpactPathHopStatus(t, resp.ImpactPath, "service", "present")
	assertImpactPathHopStatus(t, resp.ImpactPath, "environment", "missing_evidence")
}

func operationalAnchorFindingRow() SupplyChainImpactFindingRow {
	return SupplyChainImpactFindingRow{
		FindingID:           "finding-operational",
		CVEID:               "CVE-2026-1668",
		PackageID:           "pkg:npm/example",
		ImpactStatus:        "affected_exact",
		RuntimeReachability: "package_api_missing_evidence",
		RepositoryID:        "repo://example/api",
		WorkloadIDs:         []string{"workload:example-api"},
		ServiceIDs:          []string{"service:example-api"},
		Environments:        []string{"prod"},
		CatalogEntityRefs:   []string{"api:default/example-api"},
		CatalogOwnerRefs:    []string{"team:default/platform"},
		EvidencePath: []string{
			"reducer_package_consumption_correlation",
			"reducer_workload_identity",
			serviceCatalogCorrelationFactKind,
			"reducer_ci_cd_run_correlation",
		},
		EvidenceFactIDs: []string{"consume-1", "workload-1", "catalog-1", "deploy-1"},
		MissingEvidence: []string{
			"javascript/typescript parser or SCIP package API evidence missing",
		},
	}
}

func catalogEntityOperationalFindingRow() SupplyChainImpactFindingRow {
	return SupplyChainImpactFindingRow{
		FindingID:           "finding-catalog-entity",
		CVEID:               "CVE-2026-1693",
		PackageID:           "pkg:npm/example",
		ImpactStatus:        "affected_exact",
		RuntimeReachability: "package_api_missing_evidence",
		RepositoryID:        "repo://example/api",
		WorkloadIDs:         []string{"workload:example-api"},
		DeploymentIDs:       []string{"deployment:example-api"},
		CatalogEntityRefs:   []string{"api:default/example-api"},
		CatalogOwnerRefs:    []string{"team:default/platform"},
		EvidencePath: []string{
			"reducer_package_consumption_correlation",
			"reducer_workload_identity",
			serviceCatalogCorrelationFactKind,
			"reducer_platform_materialization",
		},
		EvidenceFactIDs: []string{"consume-1", "workload-1", "catalog-1", "deployment-1"},
		MissingEvidence: []string{
			"environment evidence missing",
			serviceCatalogAnchorMissingReason,
		},
	}
}

func assertImpactPathHopStatus(
	t *testing.T,
	impactPath []SupplyChainImpactPathHop,
	wantHop string,
	wantStatus string,
) {
	t.Helper()
	for _, hop := range impactPath {
		if hop.Hop != wantHop {
			continue
		}
		if hop.Status != wantStatus {
			t.Fatalf("impact_path hop %q status = %q, want %q", wantHop, hop.Status, wantStatus)
		}
		return
	}
	t.Fatalf("impact_path missing hop %q: %#v", wantHop, impactPath)
}
