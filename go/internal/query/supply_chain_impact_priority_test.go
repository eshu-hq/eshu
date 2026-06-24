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

func TestSupplyChainListImpactFindingsFiltersAndSortsByPriority(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{
			{
				FindingID:           "finding-critical",
				CVEID:               "CVE-2026-3001",
				ImpactStatus:        "possibly_affected",
				PriorityScore:       87,
				PriorityBucket:      "critical",
				PriorityReasonCodes: []string{"cisa_kev", "runtime_reachable"},
				PriorityContributions: []SupplyChainImpactPriorityContribution{
					{ReasonCode: "cisa_kev", Input: "kev", Value: "true", Contribution: 25},
				},
			},
		},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/api&priority_bucket=critical&min_priority_score=80&sort=priority_score_desc&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.PriorityBucket, "critical"; got != want {
		t.Fatalf("PriorityBucket = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.MinPriorityScore, 80; got != want {
		t.Fatalf("MinPriorityScore = %d, want %d", got, want)
	}
	if got, want := store.lastFilter.Sort, "priority_score_desc"; got != want {
		t.Fatalf("Sort = %q, want %q", got, want)
	}

	var resp struct {
		Findings []SupplyChainImpactFindingResult `json:"findings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.Findings[0].PriorityScore, 87; got != want {
		t.Fatalf("PriorityScore = %d, want %d", got, want)
	}
	if got, want := resp.Findings[0].PriorityBucket, "critical"; got != want {
		t.Fatalf("PriorityBucket = %q, want %q", got, want)
	}
	if len(resp.Findings[0].PriorityContributions) != 1 {
		t.Fatalf("PriorityContributions = %#v, want one contribution", resp.Findings[0].PriorityContributions)
	}
}

func TestSupplyChainListImpactFindingsRejectsInvalidPriorityFilters(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{ImpactFindings: &recordingSupplyChainImpactFindingStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/api&limit=10&priority_bucket=urgent",
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/api&limit=10&min_priority_score=101",
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/api&limit=10&sort=severity",
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

func TestSupplyChainListImpactFindingsRejectsZeroMinPriorityAsOnlyScope(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{ImpactFindings: &recordingSupplyChainImpactFindingStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?limit=10&min_priority_score=0",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "min_priority_score > 0") {
		t.Fatalf("body = %q, want min_priority_score > 0 scope guidance", w.Body.String())
	}
}

func TestDecodeSupplyChainImpactFindingRowPreservesPriority(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"cve_id": "CVE-2026-3002",
		"impact_status": "possibly_affected",
		"priority_score": 72,
		"priority_bucket": "high",
		"priority_reason": "high priority triage score; impact_status remains possibly_affected",
		"priority_reason_codes": ["cvss_v4_high", "range_only_version_evidence"],
		"priority_contributions": [
			{"reason_code": "cvss_v4_high", "input": "cvss", "value": "8.8", "contribution": 25},
			{"reason_code": "range_only_version_evidence", "input": "version_evidence", "value": "range_only", "contribution": -10}
		]
	}`)

	row, err := decodeSupplyChainImpactFindingRow("finding-priority", "inferred", payload)
	if err != nil {
		t.Fatalf("decodeSupplyChainImpactFindingRow() error = %v", err)
	}
	if got, want := row.PriorityScore, 72; got != want {
		t.Fatalf("PriorityScore = %d, want %d", got, want)
	}
	if got, want := row.PriorityBucket, "high"; got != want {
		t.Fatalf("PriorityBucket = %q, want %q", got, want)
	}
	if len(row.PriorityReasonCodes) != 2 {
		t.Fatalf("PriorityReasonCodes = %#v, want 2 codes", row.PriorityReasonCodes)
	}
	if len(row.PriorityContributions) != 2 || row.PriorityContributions[1].Contribution != -10 {
		t.Fatalf("PriorityContributions = %#v, want decoded signed contributions", row.PriorityContributions)
	}
}

func TestSupplyChainImpactFindingQuerySupportsPriorityFiltersAndSort(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.payload->>'priority_bucket' = $14",
		"COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) >= $15",
		"$18 = 'priority_score_desc'",
		"$18 = 'priority_score_asc'",
		"fact_id ASC",
	} {
		if !strings.Contains(listSupplyChainImpactFindingsQuery, want) {
			t.Fatalf("listSupplyChainImpactFindingsQuery missing %q:\n%s", want, listSupplyChainImpactFindingsQuery)
		}
	}
}
