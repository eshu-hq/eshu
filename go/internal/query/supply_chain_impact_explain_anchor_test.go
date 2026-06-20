package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestSupplyChainExplainImpactAcceptsWorkloadAndServiceAnchors(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactExplanationStore{
		err: ErrSupplyChainImpactExplanationNotFound,
	}
	handler := &SupplyChainHandler{ImpactExplanations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/explain?cve_id=CVE-2026-3177&image_ref=registry.example/api:prod&workload_id=workload:api&service_id=service:api",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.WorkloadID, "workload:api"; got != want {
		t.Fatalf("WorkloadID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.ServiceID, "service:api"; got != want {
		t.Fatalf("ServiceID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.ImageRef, "registry.example/api:prod"; got != want {
		t.Fatalf("ImageRef = %q, want %q", got, want)
	}

	var resp SupplyChainImpactExplanationResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !strings.HasPrefix(resp.EvidencePacketHandle, "supply-chain-impact-explanation:scope:") {
		t.Fatalf("EvidencePacketHandle = %q, want hashed bounded scope handle", resp.EvidencePacketHandle)
	}
	if got, want := resp.Anchors.Workloads, []string{"workload:api"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("Anchors.Workloads = %#v, want %#v", got, want)
	}
}

func TestSupplyChainExplainImpactNoEvidenceSurfacesUnsupportedEcosystem(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactExplanationStore{
		err: ErrSupplyChainImpactExplanationNotFound,
	}
	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 2, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: FreshnessLabelFresh},
			},
			UnsupportedTargets: []SupplyChainImpactUnsupportedTarget{
				{TargetKind: UnsupportedTargetKindEcosystem, Reason: "unsupported_ecosystem", Ecosystem: "pypi", Count: 1},
			},
		},
	}
	handler := &SupplyChainHandler{ImpactExplanations: store, Readiness: readiness}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/explain?cve_id=CVE-2026-3177&package_id=pkg:pypi/example",
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
	if got, want := resp.Outcome, "no_finding"; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if got, want := resp.Readiness.State, ReadinessStateUnsupported; got != want {
		t.Fatalf("Readiness.State = %q, want %q", got, want)
	}
	if !readinessMissingContains(resp.MissingEvidence, MissingEvidenceUnsupportedTargets) {
		t.Fatalf("MissingEvidence = %#v, want unsupported target reason", resp.MissingEvidence)
	}
	if len(resp.Readiness.UnsupportedTargets) != 1 ||
		resp.Readiness.UnsupportedTargets[0].Reason != "unsupported_ecosystem" {
		t.Fatalf("UnsupportedTargets = %#v, want unsupported ecosystem", resp.Readiness.UnsupportedTargets)
	}
}

func TestSupplyChainExplainImpactNoEvidenceSurfacesPermissionHiddenSourceState(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactExplanationStore{
		err: ErrSupplyChainImpactExplanationNotFound,
	}
	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: SupplyChainImpactReadinessSnapshot{
			SourceStates: []SupplyChainImpactSourceState{
				{
					ScopeID:        "vuln-intel://osv/npm/example",
					Source:         "osv",
					Ecosystem:      "npm",
					FreshnessState: "partial",
					TerminalStatus: "partial",
					LastErrorClass: "permission_hidden",
					WarningCount:   1,
				},
			},
		},
	}
	handler := &SupplyChainHandler{ImpactExplanations: store, Readiness: readiness}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/explain?cve_id=CVE-2026-3177&package_id=pkg:npm/example",
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
	if got, want := resp.Readiness.State, ReadinessStateTargetIncomplete; got != want {
		t.Fatalf("Readiness.State = %q, want %q", got, want)
	}
	if len(resp.Readiness.SourceStates) != 1 {
		t.Fatalf("SourceStates = %#v, want one permission-hidden partial source", resp.Readiness.SourceStates)
	}
	if got, want := resp.Readiness.SourceStates[0].LastErrorClass, "permission_hidden"; got != want {
		t.Fatalf("LastErrorClass = %q, want %q", got, want)
	}
	if got, want := resp.Readiness.IncompleteReasons, []string{"osv:partial"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("IncompleteReasons = %#v, want %#v", got, want)
	}
}

func TestSupplyChainExplainImpactNoEvidenceDoesNotMarkDerivedAnchorReady(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactExplanationStore{
		err: ErrSupplyChainImpactExplanationNotFound,
	}
	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 2, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: FreshnessLabelFresh},
				{Family: EvidenceFamilyPackageRegistry, FactCount: 1, Freshness: FreshnessLabelFresh},
			},
		},
	}
	handler := &SupplyChainHandler{ImpactExplanations: store, Readiness: readiness}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/explain?cve_id=CVE-2026-3177&workload_id=workload:api",
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
	if got, want := resp.Readiness.State, ReadinessStateEvidenceIncomplete; got != want {
		t.Fatalf("Readiness.State = %q, want %q", got, want)
	}
	if !readinessMissingContains(resp.MissingEvidence, serviceCatalogAnchorMissingReason) {
		t.Fatalf("MissingEvidence = %#v, want %q", resp.MissingEvidence, serviceCatalogAnchorMissingReason)
	}
	if !readinessMissingContains(resp.Readiness.MissingEvidence, serviceCatalogAnchorMissingReason) {
		t.Fatalf("Readiness.MissingEvidence = %#v, want %q", resp.Readiness.MissingEvidence, serviceCatalogAnchorMissingReason)
	}
}

func TestSupplyChainExplainImpactQueryFiltersWorkloadAndServiceAnchors(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"$8 = '' OR fact.payload->'workload_ids' ? $8",
		"$9 = '' OR fact.payload->'service_ids' ? $9",
		"$10 = '' OR fact.payload->>'image_ref' = $10",
	} {
		if !strings.Contains(explainSupplyChainImpactFindingQuery, want) {
			t.Fatalf("explainSupplyChainImpactFindingQuery missing %q:\n%s", want, explainSupplyChainImpactFindingQuery)
		}
	}
}
