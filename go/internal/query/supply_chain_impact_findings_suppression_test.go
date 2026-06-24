// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSupplyChainListImpactFindingsDefaultsExcludeOperatorSuppressions(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{
			{FindingID: "finding-active", CVEID: "CVE-2026-0001", ImpactStatus: "affected_exact"},
		},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastFilter.IncludeSuppressed {
		t.Fatalf("IncludeSuppressed = true, want false default for excluding operator suppressions")
	}
	if store.lastFilter.SuppressionState != "" {
		t.Fatalf("SuppressionState = %q, want empty default", store.lastFilter.SuppressionState)
	}
}

func TestSupplyChainListImpactFindingsHonorsIncludeSuppressedTrue(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{
			{
				FindingID:    "finding-not-affected",
				CVEID:        "CVE-2026-0001",
				ImpactStatus: "affected_exact",
				Suppression: &SupplyChainSuppressionDecisionRow{
					State:         "not_affected",
					SuppressionID: "suppression-1",
					Source:        "vex_statement",
					Justification: "not_affected",
					Reason:        "vulnerable function never called",
				},
			},
		},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=10&include_suppressed=true", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !store.lastFilter.IncludeSuppressed {
		t.Fatalf("IncludeSuppressed = false, want true")
	}
	var resp struct {
		Findings []SupplyChainImpactFindingResult `json:"findings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(resp.Findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1", len(resp.Findings))
	}
	if resp.Findings[0].Suppression == nil || resp.Findings[0].Suppression.State != "not_affected" {
		t.Fatalf("Suppression = %#v, want not_affected", resp.Findings[0].Suppression)
	}
	if resp.Findings[0].Suppression.Reason == "" {
		t.Fatalf("Suppression.Reason = empty, want explanation surfaced to caller")
	}
}

func TestSupplyChainListImpactFindingsRejectsUnknownSuppressionState(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{ImpactFindings: &recordingSupplyChainImpactFindingStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=10&suppression_state=bogus", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestSupplyChainListImpactFindingsRejectsInvalidIncludeSuppressed(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{ImpactFindings: &recordingSupplyChainImpactFindingStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=10&include_suppressed=maybe", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestSupplyChainListImpactFindingsFiltersBySuppressionState(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{
			{
				FindingID:    "finding-provider",
				CVEID:        "CVE-2026-0040",
				ImpactStatus: "affected_exact",
				Suppression: &SupplyChainSuppressionDecisionRow{
					State:         "provider_dismissed",
					SuppressionID: "suppression-provider",
					Source:        "provider_dismissal",
				},
			},
		},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0040&limit=10&suppression_state=provider_dismissed", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.SuppressionState, "provider_dismissed"; got != want {
		t.Fatalf("SuppressionState = %q, want %q", got, want)
	}
}

func TestListSupplyChainImpactFindingsQueryHandlesSuppressionPredicates(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') = $20",
		"$21::boolean OR COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') NOT IN ('not_affected','accepted_risk','false_positive','ignored')",
	} {
		if !strings.Contains(listSupplyChainImpactFindingsQuery, want) {
			t.Fatalf("listSupplyChainImpactFindingsQuery missing suppression predicate %q:\n%s", want, listSupplyChainImpactFindingsQuery)
		}
	}
}

func TestDecodeSupplyChainImpactFindingRowDecodesSuppressionBlock(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
        "cve_id": "CVE-2026-0001",
        "impact_status": "affected_exact",
        "suppression_state": "accepted_risk",
        "suppression": {
            "state": "accepted_risk",
            "suppression_id": "suppression-accepted",
            "source": "eshu_policy",
            "justification": "accepted_risk",
            "author": "eshu:policy/operator@acme.com",
            "authored_at": "2026-05-10T00:00:00Z",
            "reason": "compensating control deployed at gateway"
        }
    }`)

	row, err := decodeSupplyChainImpactFindingRow("finding-1", "inferred", payload)
	if err != nil {
		t.Fatalf("decodeSupplyChainImpactFindingRow() error = %v", err)
	}
	if row.Suppression == nil {
		t.Fatal("Suppression = nil, want decoded suppression block")
	}
	if row.Suppression.State != "accepted_risk" {
		t.Fatalf("Suppression.State = %q, want accepted_risk", row.Suppression.State)
	}
	if row.Suppression.SuppressionID != "suppression-accepted" {
		t.Fatalf("Suppression.SuppressionID = %q, want suppression-accepted", row.Suppression.SuppressionID)
	}
	if row.Suppression.Author != "eshu:policy/operator@acme.com" {
		t.Fatalf("Suppression.Author = %q, want preserved", row.Suppression.Author)
	}
}

func TestDecodeSupplyChainImpactFindingRowFallsBackToTopLevelState(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
        "cve_id": "CVE-2026-0001",
        "impact_status": "affected_exact",
        "suppression_state": "active"
    }`)
	row, err := decodeSupplyChainImpactFindingRow("finding-2", "inferred", payload)
	if err != nil {
		t.Fatalf("decodeSupplyChainImpactFindingRow() error = %v", err)
	}
	if row.Suppression == nil || row.Suppression.State != "active" {
		t.Fatalf("Suppression = %#v, want top-level state to populate active row", row.Suppression)
	}
}
