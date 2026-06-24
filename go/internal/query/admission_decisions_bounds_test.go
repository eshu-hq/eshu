// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdmissionDecisionUnsupportedProfileReturnsContractErrorBeforeStoreRead(t *testing.T) {
	t.Parallel()

	store := &failingAdmissionDecisionReadStore{}
	handler := &EvidenceHandler{
		AdmissionDecisions: store,
		Profile:            ProfileLocalLightweight,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/evidence/admission-decisions?domain=deployable_unit&scope_id=git-repository-scope:team/api&generation_id=generation-1&limit=5",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.called {
		t.Fatal("store was called before unsupported-capability profile gate")
	}
	if !strings.Contains(rec.Body.String(), `"unsupported_capability"`) {
		t.Fatalf("body = %s, want unsupported_capability envelope", rec.Body.String())
	}
}

func TestAdmissionDecisionHandlerCapsIncludedEvidencePerDecision(t *testing.T) {
	t.Parallel()

	evidence := make([]AdmissionDecisionEvidenceRow, 0, 25)
	for i := range 25 {
		evidence = append(evidence, AdmissionDecisionEvidenceRow{
			EvidenceID:   "evidence-extra",
			DecisionID:   "decision-1",
			SourceHandle: "fact:relationship:1",
			EvidenceKind: "relationship_fact",
			Detail:       map[string]any{"ordinal": i},
			CreatedAt:    time.Unix(1700000000+int64(i), 0).UTC(),
		})
	}
	store := &recordingAdmissionDecisionReadStore{
		rows: []AdmissionDecisionReadRow{
			admissionDecisionReadRow("decision-1", "admitted"),
		},
		evidence: map[string][]AdmissionDecisionEvidenceRow{
			"decision-1": evidence,
		},
	}
	handler := &EvidenceHandler{
		AdmissionDecisions: store,
		Profile:            ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/evidence/admission-decisions?domain=deployable_unit&scope_id=git-repository-scope:team/api&generation_id=generation-1&limit=5&include_evidence=true",
		nil,
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := store.lastEvidenceLimit, 21; got != want {
		t.Fatalf("evidence limit = %d, want %d for limit+1 truncation proof", got, want)
	}
	var body struct {
		Decisions []AdmissionDecisionResult `json:"decisions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, rec.Body.String())
	}
	if len(body.Decisions) != 1 {
		t.Fatalf("len(decisions) = %d, want 1", len(body.Decisions))
	}
	decision := body.Decisions[0]
	if got, want := len(decision.Evidence), 20; got != want {
		t.Fatalf("len(evidence) = %d, want %d", got, want)
	}
	if decision.EvidenceLimit != 20 {
		t.Fatalf("evidence_limit = %d, want 20", decision.EvidenceLimit)
	}
	if decision.EvidenceTruncated == nil || !*decision.EvidenceTruncated {
		t.Fatalf("evidence_truncated = %#v, want true", decision.EvidenceTruncated)
	}
}
