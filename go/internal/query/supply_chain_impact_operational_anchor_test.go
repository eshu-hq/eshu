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
	assertImpactPathHopStatus(t, resp.ImpactPath, "service", "present")
	assertImpactPathHopStatus(t, resp.ImpactPath, "environment", "present")
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
