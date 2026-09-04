// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

// Test 8: environment_evidence round-trips through the actual HTTP findings
// route response body -- decoded from raw JSON bytes into a fresh anonymous
// type, the way an external caller would, rather than by reusing
// SupplyChainImpactFindingResult on both sides of the assertion.
func TestSupplyChainImpactFindingsExposeEnvironmentEvidenceInResponseBody(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []impact.SupplyChainImpactFindingRow{
			{
				FindingID:    "finding-env-evidence",
				CVEID:        "CVE-2026-5426",
				ImpactStatus: "affected_exact",
				Confidence:   "exact",
				RepositoryID: "repo://example/svc",
				Environments: []string{"prod", "staging"},
				EnvironmentEvidence: map[string]string{
					"prod":    "deploy_event",
					"staging": "declared",
				},
			},
		},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/svc&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Findings []struct {
			EnvironmentEvidence map[string]string `json:"environment_evidence"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v; body = %s", err, w.Body.String())
	}
	if len(resp.Findings) != 1 {
		t.Fatalf("Findings length = %d, want 1; body = %s", len(resp.Findings), w.Body.String())
	}
	if got, want := resp.Findings[0].EnvironmentEvidence["prod"], "deploy_event"; got != want {
		t.Fatalf("environment_evidence[prod] = %q, want %q", got, want)
	}
	if got, want := resp.Findings[0].EnvironmentEvidence["staging"], "declared"; got != want {
		t.Fatalf("environment_evidence[staging] = %q, want %q", got, want)
	}
}

// A finding with no recorded environment evidence -- including any row written
// before #5426 landed -- must OMIT environment_evidence from the wire rather
// than emit an empty object. The struct tag carries omitempty, so this pins the
// documented contract in the HTTP API reference and the OpenAPI description
// against a future tag change that would silently start serving "{}".
func TestSupplyChainImpactFindingsOmitEnvironmentEvidenceWhenAbsent(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []impact.SupplyChainImpactFindingRow{
			{
				FindingID:    "finding-no-env-evidence",
				CVEID:        "CVE-2026-5426",
				ImpactStatus: "affected_exact",
				Confidence:   "exact",
				RepositoryID: "repo://example/svc",
			},
		},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/svc&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	// Positive control: without this, the assertion below would also pass on a
	// response that returned no findings at all.
	var resp struct {
		Findings []map[string]any `json:"findings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v; body = %s", err, w.Body.String())
	}
	if len(resp.Findings) != 1 {
		t.Fatalf("Findings length = %d, want 1; body = %s", len(resp.Findings), w.Body.String())
	}
	if strings.Contains(w.Body.String(), "environment_evidence") {
		t.Fatalf("body contains environment_evidence for a finding with none; body = %s", w.Body.String())
	}
}

// Test 10: environment_evidence survives the PERSISTED-PAYLOAD decode.
//
// Tests 8 and 9 seed SupplyChainImpactFindingRow directly, so they only cover
// the response half. Without this, a typo in the payload key that silently
// decodes to nil would leave every package green while the reducer's evidence
// never reached a caller -- the reducer-writes/API-never-reads shape, which the
// golden corpus cannot catch here either (#5836).
func TestDecodeSupplyChainImpactFindingRowDecodesEnvironmentEvidence(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"cve_id": "CVE-2026-5426",
		"impact_status": "affected_exact",
		"environments": ["prod", "staging"],
		"environment_evidence": {"prod": "deploy_event", "staging": "declared"}
	}`)

	row, err := impact.DecodeSupplyChainImpactFindingRow("finding-1", "exact", payload)
	if err != nil {
		t.Fatalf("impact.DecodeSupplyChainImpactFindingRow() error = %v", err)
	}
	if got, want := row.EnvironmentEvidence["prod"], "deploy_event"; got != want {
		t.Fatalf("EnvironmentEvidence[prod] = %q, want %q", got, want)
	}
	if got, want := row.EnvironmentEvidence["staging"], "declared"; got != want {
		t.Fatalf("EnvironmentEvidence[staging] = %q, want %q", got, want)
	}
}

// Test 11: a payload with no environment_evidence decodes to a nil map rather
// than failing or fabricating entries. This is every row written before #5426,
// and it is what lets the response omit the field.
func TestDecodeSupplyChainImpactFindingRowToleratesAbsentEnvironmentEvidence(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"cve_id": "CVE-2026-5426",
		"impact_status": "affected_exact",
		"environments": ["prod"]
	}`)

	row, err := impact.DecodeSupplyChainImpactFindingRow("finding-1", "exact", payload)
	if err != nil {
		t.Fatalf("impact.DecodeSupplyChainImpactFindingRow() error = %v", err)
	}
	if row.EnvironmentEvidence != nil {
		t.Fatalf("EnvironmentEvidence = %#v, want nil for a row predating #5426", row.EnvironmentEvidence)
	}
}
