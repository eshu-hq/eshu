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

func TestSupplyChainListImpactFindingsDefaultsToPreciseProfile(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-9001&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.DetectionProfile, SupplyChainImpactProfilePrecise; got != want {
		t.Fatalf("filter.DetectionProfile = %q, want %q", got, want)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp["detection_profile"], SupplyChainImpactProfilePrecise; got != want {
		t.Fatalf("detection_profile = %#v, want %q", got, want)
	}
}

func TestSupplyChainListImpactFindingsComprehensiveDoesNotFilterDownstream(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{
			{
				FindingID:        "finding-precise",
				CVEID:            "CVE-2026-9001",
				ImpactStatus:     "affected_exact",
				DetectionProfile: SupplyChainImpactProfilePrecise,
			},
			{
				FindingID:        "finding-comprehensive",
				CVEID:            "CVE-2026-9001",
				ImpactStatus:     "possibly_affected",
				MatchReason:      "range_only_manifest",
				DetectionProfile: SupplyChainImpactProfileComprehensive,
			},
		},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-9001&limit=10&profile=comprehensive",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got := store.lastFilter.DetectionProfile; got != "" {
		t.Fatalf("filter.DetectionProfile = %q, want blank so comprehensive accepts every row", got)
	}

	var resp struct {
		Findings         []SupplyChainImpactFindingResult `json:"findings"`
		DetectionProfile string                           `json:"detection_profile"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.DetectionProfile, SupplyChainImpactProfileComprehensive; got != want {
		t.Fatalf("detection_profile = %q, want %q", got, want)
	}
	if got, want := len(resp.Findings), 2; got != want {
		t.Fatalf("len(findings) = %d, want %d", got, want)
	}
	seen := map[string]string{}
	for _, finding := range resp.Findings {
		seen[finding.FindingID] = finding.DetectionProfile
	}
	if seen["finding-precise"] != SupplyChainImpactProfilePrecise {
		t.Fatalf("precise row profile = %q, want %q", seen["finding-precise"], SupplyChainImpactProfilePrecise)
	}
	if seen["finding-comprehensive"] != SupplyChainImpactProfileComprehensive {
		t.Fatalf("comprehensive row profile = %q, want %q", seen["finding-comprehensive"], SupplyChainImpactProfileComprehensive)
	}
	for _, finding := range resp.Findings {
		if finding.DetectionProfile == SupplyChainImpactProfileComprehensive && finding.MatchReason == "" {
			t.Fatalf("comprehensive row %q must keep an explicit match_reason", finding.FindingID)
		}
	}
}

func TestSupplyChainListImpactFindingsRejectsUnknownProfile(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-9001&limit=10&profile=ludicrous",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "precise or comprehensive") {
		t.Fatalf("body = %q, want guidance on valid profile values", w.Body.String())
	}
}

func TestSupplyChainImpactFindingQueryUsesDetectionProfileFilter(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.payload->>'detection_profile' = $13",
		"$13 = 'comprehensive'",
		"$13 = 'precise'",
		"npm_semver_affected_range",
		"npm_semver_known_fixed",
		"nuget_semver_affected_range",
		"nuget_semver_known_fixed",
		"cargo_semver_affected_range",
		"cargo_semver_known_fixed",
		"hex_semver_affected_range",
		"hex_semver_known_fixed",
		"maven_range_match",
		"maven_known_fixed",
		"swift_semver_affected_range",
		"swift_semver_known_fixed",
	} {
		if !strings.Contains(listSupplyChainImpactFindingsQuery, want) {
			t.Fatalf("listSupplyChainImpactFindingsQuery missing %q:\n%s", want, listSupplyChainImpactFindingsQuery)
		}
	}
}

func TestDecodeSupplyChainImpactFindingRowBackfillsLegacyPreciseProfile(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload []byte
	}{
		{
			name: "npm_semver",
			payload: []byte(`{
                "cve_id": "CVE-2026-9001",
                "impact_status": "affected_exact",
                "match_reason": "npm_semver_affected_range",
                "observed_version": "1.2.3"
            }`),
		},
		{
			name: "nuget_semver",
			payload: []byte(`{
                "cve_id": "CVE-2026-9003",
                "impact_status": "not_affected_known_fixed",
                "match_reason": "nuget_semver_known_fixed",
                "observed_version": "13.0.4"
            }`),
		},
		{
			name: "cargo_semver",
			payload: []byte(`{
                "cve_id": "CVE-2026-9004",
                "impact_status": "affected_exact",
                "match_reason": "cargo_semver_affected_range",
                "observed_version": "1.0.210"
            }`),
		},
		{
			name: "swift_semver",
			payload: []byte(`{
                "cve_id": "CVE-2026-9002",
                "impact_status": "affected_exact",
                "match_reason": "swift_semver_affected_range",
                "observed_version": "4.3.0"
            }`),
		},
		{
			name: "hex_semver",
			payload: []byte(`{
                "cve_id": "CVE-2026-9005",
                "impact_status": "affected_exact",
                "match_reason": "hex_semver_affected_range",
                "observed_version": "1.2.3"
            }`),
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row, err := decodeSupplyChainImpactFindingRow("finding-legacy-precise-"+tc.name, "inferred", tc.payload)
			if err != nil {
				t.Fatalf("decodeSupplyChainImpactFindingRow() error = %v", err)
			}
			if got, want := row.DetectionProfile, SupplyChainImpactProfilePrecise; got != want {
				t.Fatalf("DetectionProfile = %q, want %q for legacy fact qualifying as precise", got, want)
			}
		})
	}
}

func TestDecodeSupplyChainImpactFindingRowBackfillsLegacyComprehensiveProfile(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload []byte
	}{
		{
			name: "range_only_manifest",
			payload: []byte(`{
                "impact_status": "possibly_affected",
                "match_reason": "range_only_manifest"
            }`),
		},
		{
			name: "missing_observed_version",
			payload: []byte(`{
                "impact_status": "affected_exact",
                "match_reason": "npm_semver_affected_range"
            }`),
		},
		{
			name: "unsupported_ecosystem",
			payload: []byte(`{
                "impact_status": "possibly_affected",
                "match_reason": "unsupported_ecosystem",
                "observed_version": "1.0.0"
            }`),
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row, err := decodeSupplyChainImpactFindingRow("finding-legacy-"+tc.name, "inferred", tc.payload)
			if err != nil {
				t.Fatalf("decodeSupplyChainImpactFindingRow() error = %v", err)
			}
			if got, want := row.DetectionProfile, SupplyChainImpactProfileComprehensive; got != want {
				t.Fatalf("DetectionProfile = %q, want %q", got, want)
			}
		})
	}
}

func TestDecodeSupplyChainImpactFindingRowPreservesDetectionProfile(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
            "cve_id": "CVE-2026-9001",
            "impact_status": "affected_exact",
            "match_reason": "npm_semver_affected_range",
            "detection_profile": "precise"
        }`)

	row, err := decodeSupplyChainImpactFindingRow("finding-1", "inferred", payload)
	if err != nil {
		t.Fatalf("decodeSupplyChainImpactFindingRow() error = %v", err)
	}
	if got, want := row.DetectionProfile, SupplyChainImpactProfilePrecise; got != want {
		t.Fatalf("DetectionProfile = %q, want %q", got, want)
	}
}
