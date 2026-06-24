// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSupplyChainExplainImpactAmbiguousScope(t *testing.T) {
	t.Parallel()

	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []SupplyChainImpactEvidenceFamily{
				{Family: EvidenceFamilyPackageConsumption, FactCount: 2, Freshness: FreshnessLabelFresh},
			},
		},
	}
	store := &recordingSupplyChainImpactExplanationStore{
		err: ErrSupplyChainImpactExplanationAmbiguous,
	}
	handler := &SupplyChainHandler{ImpactExplanations: store, Readiness: readiness}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/explain?advisory_id=GHSA-ambiguous&repository_id=repo://example/api",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var envelope struct {
		Data  SupplyChainImpactExplanationResult `json:"data"`
		Truth *TruthEnvelope                     `json:"truth"`
		Error *ErrorEnvelope                     `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("error = %#v, want nil", envelope.Error)
	}
	if envelope.Truth == nil {
		t.Fatal("truth = nil, want canonical truth envelope")
	}
	if got, want := envelope.Truth.Capability, supplyChainImpactExplanationCapability; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
	resp := envelope.Data
	if got, want := resp.Outcome, "ambiguous_scope"; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if resp.Finding != nil {
		t.Fatalf("Finding = %#v, want nil", resp.Finding)
	}
	if got, want := resp.Version.VersionEvidence, "missing"; got != want {
		t.Fatalf("Version.VersionEvidence = %q, want %q", got, want)
	}
	if len(resp.Evidence) != 0 {
		t.Fatalf("Evidence = %#v, want empty bounded refusal evidence list", resp.Evidence)
	}
	if len(resp.ImpactPath) != 0 {
		t.Fatalf("ImpactPath = %#v, want no fabricated impact path", resp.ImpactPath)
	}
	if got, want := resp.Anchors.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("Anchors.RepositoryID = %q, want %q", got, want)
	}
	if !containsString(resp.MissingEvidence, "ambiguous_scope") {
		t.Fatalf("MissingEvidence = %#v, want ambiguous_scope", resp.MissingEvidence)
	}
	if got, want := resp.Readiness.State, ReadinessStateAmbiguousScope; got != want {
		t.Fatalf("Readiness.State = %q, want %q", got, want)
	}
	if got, want := resp.Readiness.Counts.FindingsReturned, 2; got != want {
		t.Fatalf("Readiness.Counts.FindingsReturned = %d, want %d so ambiguous scopes are not reported as zero findings", got, want)
	}
	if got, want := resp.Readiness.TargetScope.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("Readiness.TargetScope.RepositoryID = %q, want %q", got, want)
	}
	if !containsString(resp.Readiness.MissingEvidence, "ambiguous_scope") {
		t.Fatalf("Readiness.MissingEvidence = %#v, want ambiguous_scope", resp.Readiness.MissingEvidence)
	}
}

func TestSupplyChainImpactAmbiguousExplanationUsesCandidateCount(t *testing.T) {
	t.Parallel()

	body := BuildSupplyChainImpactAmbiguousExplanation(
		SupplyChainImpactExplanationFilter{AdvisoryID: "GHSA-ambiguous", RepositoryID: "repo://example/api"},
		SupplyChainImpactReadinessEnvelope{State: ReadinessStateReadyZeroFindings},
		4,
	)
	if got, want := body.Readiness.State, ReadinessStateAmbiguousScope; got != want {
		t.Fatalf("Readiness.State = %q, want %q", got, want)
	}
	if got, want := body.Readiness.Counts.FindingsReturned, 4; got != want {
		t.Fatalf("Readiness.Counts.FindingsReturned = %d, want %d", got, want)
	}
}
