// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

func TestSupplyChainExplainImpactAcceptsWorkloadAndServiceAnchors(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactExplanationStore{
		err: impact.ErrSupplyChainImpactExplanationNotFound,
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

	var resp impact.SupplyChainImpactExplanationResult
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
		err: impact.ErrSupplyChainImpactExplanationNotFound,
	}
	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: impact.SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []impact.SupplyChainImpactEvidenceFamily{
				{Family: impact.EvidenceFamilyVulnerabilityAdvisory, FactCount: 2, Freshness: impact.FreshnessLabelFresh},
				{Family: impact.EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: impact.FreshnessLabelFresh},
			},
			UnsupportedTargets: []impact.SupplyChainImpactUnsupportedTarget{
				{TargetKind: impact.UnsupportedTargetKindEcosystem, Reason: "unsupported_ecosystem", Ecosystem: "pypi", Count: 1},
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

	var resp impact.SupplyChainImpactExplanationResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, want := resp.Outcome, "no_finding"; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if got, want := resp.Readiness.State, impact.ReadinessStateUnsupported; got != want {
		t.Fatalf("Readiness.State = %q, want %q", got, want)
	}
	if !impact.ReadinessMissingContains(resp.MissingEvidence, impact.MissingEvidenceUnsupportedTargets) {
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
		err: impact.ErrSupplyChainImpactExplanationNotFound,
	}
	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: impact.SupplyChainImpactReadinessSnapshot{
			SourceStates: []impact.SupplyChainImpactSourceState{
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

	var resp impact.SupplyChainImpactExplanationResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, want := resp.Readiness.State, impact.ReadinessStateTargetIncomplete; got != want {
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
		err: impact.ErrSupplyChainImpactExplanationNotFound,
	}
	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: impact.SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []impact.SupplyChainImpactEvidenceFamily{
				{Family: impact.EvidenceFamilyVulnerabilityAdvisory, FactCount: 2, Freshness: impact.FreshnessLabelFresh},
				{Family: impact.EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: impact.FreshnessLabelFresh},
				{Family: impact.EvidenceFamilyPackageRegistry, FactCount: 1, Freshness: impact.FreshnessLabelFresh},
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

	var resp impact.SupplyChainImpactExplanationResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, want := resp.Readiness.State, impact.ReadinessStateEvidenceIncomplete; got != want {
		t.Fatalf("Readiness.State = %q, want %q", got, want)
	}
	if !impact.ReadinessMissingContains(resp.MissingEvidence, impact.ServiceCatalogAnchorMissingReason) {
		t.Fatalf("MissingEvidence = %#v, want %q", resp.MissingEvidence, impact.ServiceCatalogAnchorMissingReason)
	}
	if !impact.ReadinessMissingContains(resp.Readiness.MissingEvidence, impact.ServiceCatalogAnchorMissingReason) {
		t.Fatalf("Readiness.MissingEvidence = %#v, want %q", resp.Readiness.MissingEvidence, impact.ServiceCatalogAnchorMissingReason)
	}
}

func TestSupplyChainExplainImpactQueryFiltersWorkloadAndServiceAnchors(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"$8 = ''",
		"runtime_filter.filter_kind = 'workload'",
		"$9 = ''",
		"runtime_filter.filter_kind = 'service'",
		"runtime_filter.repository_id = fact.payload->>'repository_id'",
		"$10 = '' OR fact.payload->>'image_ref' = $10",
	} {
		if !strings.Contains(impact.ExplainSupplyChainImpactFindingQuery, want) {
			t.Fatalf("impact.ExplainSupplyChainImpactFindingQuery missing %q:\n%s", want, impact.ExplainSupplyChainImpactFindingQuery)
		}
	}

	for _, staleMembership := range []string{
		"fact.payload->'workload_ids' ? $8",
		"fact.payload->'service_ids' ? $9",
	} {
		if strings.Contains(impact.ExplainSupplyChainImpactFindingQuery, staleMembership) {
			t.Fatalf(
				"impact.ExplainSupplyChainImpactFindingQuery contains stale baked membership %q:\n%s",
				staleMembership,
				impact.ExplainSupplyChainImpactFindingQuery,
			)
		}
	}
}
