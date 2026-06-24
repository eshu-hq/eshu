// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDecodeSupplyChainImpactFindingRowPreservesReachabilityEnvelope(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"cve_id": "CVE-2026-3701",
		"impact_status": "affected_exact",
		"confidence": "exact",
		"runtime_reachability": "not_called",
		"reachability": {
			"state": "not_called",
			"confidence": "strong",
			"source": "govulncheck",
			"evidence": "symbol_not_called",
			"reason": "govulncheck proved the vulnerable symbol is not called",
			"language_maturity": "implemented",
			"missing_evidence": ["runtime deployment evidence missing"]
		}
	}`)

	row, err := decodeSupplyChainImpactFindingRow("finding-reachability", "observed", payload)
	if err != nil {
		t.Fatalf("decodeSupplyChainImpactFindingRow() error = %v", err)
	}
	if row.ImpactStatus != "affected_exact" {
		t.Fatalf("ImpactStatus = %q, want affected_exact", row.ImpactStatus)
	}
	if row.Confidence != "exact" {
		t.Fatalf("Confidence = %q, want exact", row.Confidence)
	}
	if row.Reachability == nil {
		t.Fatal("Reachability = nil, want envelope")
	}
	if got, want := row.Reachability.State, "not_called"; got != want {
		t.Fatalf("Reachability.State = %q, want %q", got, want)
	}
	if got, want := row.Reachability.Confidence, "strong"; got != want {
		t.Fatalf("Reachability.Confidence = %q, want %q", got, want)
	}
	if got, want := row.Reachability.Source, "govulncheck"; got != want {
		t.Fatalf("Reachability.Source = %q, want %q", got, want)
	}
}

func TestDecodeSupplyChainImpactFindingRowPreservesJSTSReachabilitySeparatelyFromImpact(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"cve_id": "CVE-2026-3703",
		"package_id": "pkg:npm/@scope/vulnerable-api",
		"ecosystem": "npm",
		"impact_status": "affected_exact",
		"confidence": "exact",
		"runtime_reachability": "package_api_call",
		"reachability": {
			"state": "reachable",
			"confidence": "partial",
			"source": "parser_js_ts",
			"evidence": "package_api_call",
			"reason": "JavaScript/TypeScript parser evidence proves the package API identity is called",
			"language_maturity": "partial"
		}
	}`)

	row, err := decodeSupplyChainImpactFindingRow("finding-js-ts-reachability", "observed", payload)
	if err != nil {
		t.Fatalf("decodeSupplyChainImpactFindingRow() error = %v", err)
	}
	if row.ImpactStatus != "affected_exact" {
		t.Fatalf("ImpactStatus = %q, want affected_exact", row.ImpactStatus)
	}
	if row.Confidence != "exact" {
		t.Fatalf("Confidence = %q, want exact impact confidence", row.Confidence)
	}
	if row.Reachability == nil {
		t.Fatal("Reachability = nil, want JS/TS envelope")
	}
	if row.Reachability.Source != "parser_js_ts" {
		t.Fatalf("Reachability.Source = %q, want parser_js_ts", row.Reachability.Source)
	}
	if row.Reachability.Confidence != "partial" {
		t.Fatalf("Reachability.Confidence = %q, want partial reachability confidence", row.Reachability.Confidence)
	}
}

func TestSupplyChainImpactFindingsExposeReachabilityWithoutDowngradingImpact(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{
			{
				FindingID:           "finding-not-called",
				CVEID:               "CVE-2026-3702",
				ImpactStatus:        "affected_exact",
				Confidence:          "exact",
				RepositoryID:        "repo://example/go",
				RuntimeReachability: "not_called",
				Reachability: &SupplyChainReachabilityResult{
					State:      "not_called",
					Confidence: "strong",
					Source:     "govulncheck",
				},
			},
			{
				FindingID:           "finding-rubygems-reachable",
				CVEID:               "CVE-2026-3703",
				ImpactStatus:        "affected_exact",
				Confidence:          "exact",
				RepositoryID:        "repo://example/ruby",
				RuntimeReachability: "package_manifest",
				Reachability: &SupplyChainReachabilityResult{
					State:      "reachable",
					Confidence: "partial",
					Source:     "bundler",
					Evidence:   "bundler_dependency_path",
				},
			},
		},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/ruby&limit=10",
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
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.Findings[0].ImpactStatus, "affected_exact"; got != want {
		t.Fatalf("ImpactStatus = %q, want %q", got, want)
	}
	if resp.Findings[0].Reachability == nil {
		t.Fatal("Reachability = nil, want envelope")
	}
	if got, want := resp.Findings[0].Reachability.State, "not_called"; got != want {
		t.Fatalf("Reachability.State = %q, want %q", got, want)
	}
	if got, want := resp.Findings[0].Reachability.Source, "govulncheck"; got != want {
		t.Fatalf("Reachability.Source = %q, want %q", got, want)
	}
	if resp.Findings[1].Reachability == nil {
		t.Fatal("Reachability = nil for second finding, want envelope")
	}
	if got, want := resp.Findings[1].Reachability.State, "reachable"; got != want {
		t.Fatalf("Second Reachability.State = %q, want %q", got, want)
	}
	if got, want := resp.Findings[1].Reachability.Source, "bundler"; got != want {
		t.Fatalf("Second Reachability.Source = %q, want %q", got, want)
	}
}
