// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingAdvisoryEvidenceStore struct {
	rows       []AdvisoryEvidenceRow
	lastFilter AdvisoryEvidenceFilter
	calls      int
}

func (s *recordingAdvisoryEvidenceStore) ListAdvisoryEvidence(
	_ context.Context,
	filter AdvisoryEvidenceFilter,
) ([]AdvisoryEvidenceRow, error) {
	s.calls++
	s.lastFilter = filter
	return append([]AdvisoryEvidenceRow(nil), s.rows...), nil
}

type unusedAdvisoryEvidenceQueryer struct{}

func (unusedAdvisoryEvidenceQueryer) QueryContext(
	context.Context,
	string,
	...any,
) (*sql.Rows, error) {
	return nil, fmt.Errorf("query must not run for invalid filters")
}

func TestSupplyChainListAdvisoryEvidenceRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{AdvisoryEvidence: &recordingAdvisoryEvidenceStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/advisories/evidence?limit=10",
		"/api/v0/supply-chain/advisories/evidence?cve_id=CVE-2026-0001",
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

func TestPostgresAdvisoryEvidenceStoreReportsPaginationLimit(t *testing.T) {
	t.Parallel()

	store := NewPostgresAdvisoryEvidenceStore(unusedAdvisoryEvidenceQueryer{})

	_, err := store.ListAdvisoryEvidence(context.Background(), AdvisoryEvidenceFilter{
		CVEID: "CVE-2026-0001",
		Limit: advisoryEvidenceMaxLimit + 2,
	})
	if err == nil {
		t.Fatal("ListAdvisoryEvidence() error = nil, want limit error")
	}
	want := fmt.Sprintf("limit must be between 1 and %d for internal pagination", advisoryEvidenceMaxLimit+1)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestNormalizeAdvisoryEvidenceFilterCanonicalizesIdentityInputs(t *testing.T) {
	t.Parallel()

	got := normalizeAdvisoryEvidenceFilter(AdvisoryEvidenceFilter{
		CVEID:            " cve-2026-0001 ",
		AdvisoryID:       " gHsA-aaaa-bbbb-cccc ",
		PackageID:        " pkg:npm/example ",
		Source:           " NVD ",
		AfterAdvisoryKey: " osv-2026-0001 ",
		Limit:            10,
	})

	if got.CVEID != "CVE-2026-0001" {
		t.Fatalf("CVEID = %q, want canonical CVE", got.CVEID)
	}
	if got.AdvisoryID != "GHSA-aaaa-bbbb-cccc" {
		t.Fatalf("AdvisoryID = %q, want canonical GHSA prefix", got.AdvisoryID)
	}
	if got.AfterAdvisoryKey != "OSV-2026-0001" {
		t.Fatalf("AfterAdvisoryKey = %q, want canonical OSV prefix", got.AfterAdvisoryKey)
	}
	if got.PackageID != "pkg:npm/example" {
		t.Fatalf("PackageID = %q, want trimmed package id", got.PackageID)
	}
	if got.Source != "nvd" {
		t.Fatalf("Source = %q, want lowercase source", got.Source)
	}
	if got.Limit != 10 {
		t.Fatalf("Limit = %d, want preserved limit", got.Limit)
	}
}

func TestSupplyChainListAdvisoryEvidenceUsesBoundedStore(t *testing.T) {
	t.Parallel()

	store := &recordingAdvisoryEvidenceStore{
		rows: []AdvisoryEvidenceRow{
			{
				AdvisoryKey: "CVE-2026-0001",
				CanonicalID: "CVE-2026-0001",
				CVEIDs:      []string{"CVE-2026-0001"},
				GHSAIDs:     []string{"GHSA-aaaa-bbbb-cccc"},
				Sources: []AdvisorySourceEvidence{
					{
						Source:        "ghsa",
						AdvisoryID:    "GHSA-aaaa-bbbb-cccc",
						CVEID:         "CVE-2026-0001",
						CVSSVectorV3:  "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
						CVSSVectorV4:  "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:H/SI:H/SA:H",
						CWEs:          []string{"CWE-79"},
						SourceFactIDs: []string{"ghsa-cve"},
					},
				},
			},
			{AdvisoryKey: "CVE-2026-0002", CanonicalID: "CVE-2026-0002"},
		},
	}
	handler := &SupplyChainHandler{AdvisoryEvidence: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/advisories/evidence?advisory_id=GHSA-aaaa-bbbb-cccc&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.AdvisoryID, "GHSA-aaaa-bbbb-cccc"; got != want {
		t.Fatalf("AdvisoryID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}

	var resp struct {
		Advisories []AdvisoryEvidenceRow `json:"advisories"`
		Count      int                   `json:"count"`
		Limit      int                   `json:"limit"`
		Truncated  bool                  `json:"truncated"`
		NextCursor map[string]string     `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Advisories), 1; got != want {
		t.Fatalf("len(advisories) = %d, want %d", got, want)
	}
	if got, want := resp.Advisories[0].Sources[0].CVSSVectorV4, "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:H/SI:H/SA:H"; got != want {
		t.Fatalf("CVSSVectorV4 = %q, want %q", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_advisory_key"], "CVE-2026-0001"; got != want {
		t.Fatalf("next_cursor.after_advisory_key = %q, want %q", got, want)
	}
}

func TestPageAdvisoryEvidenceRowsNormalizesCursor(t *testing.T) {
	t.Parallel()

	rows := []AdvisoryEvidenceRow{
		{AdvisoryKey: "CVE-2026-0001"},
		{AdvisoryKey: "GHSA-aaaa-bbbb-cccc"},
		{AdvisoryKey: "OSV-2026-0001"},
	}

	got := pageAdvisoryEvidenceRows(rows, AdvisoryEvidenceFilter{
		AfterAdvisoryKey: "ghsa-AAAA-bbbb-cccc",
		Limit:            1,
	})
	if len(got) != 1 || got[0].AdvisoryKey != "OSV-2026-0001" {
		t.Fatalf("page after mixed-case GHSA = %#v, want OSV row", got)
	}

	got = pageAdvisoryEvidenceRows(rows, AdvisoryEvidenceFilter{
		AfterAdvisoryKey: "cve-2026-0001",
		Limit:            1,
	})
	if len(got) != 1 || got[0].AdvisoryKey != "GHSA-aaaa-bbbb-cccc" {
		t.Fatalf("page after lowercase CVE = %#v, want GHSA row", got)
	}
}

func TestPageAdvisoryEvidenceRowsKeepsCVEAnchorScoped(t *testing.T) {
	t.Parallel()

	rows := []AdvisoryEvidenceRow{
		{AdvisoryKey: "CVE-2026-0002", CanonicalID: "CVE-2026-0002", CVEIDs: []string{"CVE-2026-0002"}},
		{AdvisoryKey: "CVE-2026-0001", CanonicalID: "CVE-2026-0001", CVEIDs: []string{"CVE-2026-0001"}},
		{AdvisoryKey: "CVE-2026-0003", CanonicalID: "CVE-2026-0003", CVEIDs: []string{"CVE-2026-0003"}},
	}

	got := pageAdvisoryEvidenceRows(rows, AdvisoryEvidenceFilter{CVEID: "CVE-2026-0001", Limit: 10})
	if len(got) != 1 || got[0].CanonicalID != "CVE-2026-0001" {
		t.Fatalf("CVE-scoped page = %#v, want only CVE-2026-0001", got)
	}
}

func TestPageAdvisoryEvidenceRowsKeepsPackageAnchorBroad(t *testing.T) {
	t.Parallel()

	rows := []AdvisoryEvidenceRow{
		{
			AdvisoryKey: "CVE-2026-0001",
			CanonicalID: "CVE-2026-0001",
			AffectedPackages: []AdvisoryAffectedPackage{
				{PackageID: "pkg:npm/example"},
			},
		},
		{
			AdvisoryKey: "CVE-2026-0002",
			CanonicalID: "CVE-2026-0002",
			AffectedPackages: []AdvisoryAffectedPackage{
				{PackageID: "pkg:npm/example"},
			},
		},
		{
			AdvisoryKey: "CVE-2026-0003",
			CanonicalID: "CVE-2026-0003",
			AffectedPackages: []AdvisoryAffectedPackage{
				{PackageID: "pkg:npm/other"},
			},
		},
	}

	got := pageAdvisoryEvidenceRows(rows, AdvisoryEvidenceFilter{PackageID: "pkg:npm/example", Limit: 10})
	if len(got) != 2 {
		t.Fatalf("package-scoped page length = %d, want 2: %#v", len(got), got)
	}
}

func TestAdvisoryEvidenceFactCapacityUsesQueryLimit(t *testing.T) {
	t.Parallel()

	if got, want := advisoryEvidenceFactCapacity(), advisoryEvidenceMaxFactRows; got != want {
		t.Fatalf("advisoryEvidenceFactCapacity() = %d, want %d", got, want)
	}
}

func TestBuildAdvisoryEvidenceRowsMergesSourceOnlyEvidence(t *testing.T) {
	t.Parallel()

	rows := []advisoryEvidenceFactRow{
		factRow("ghsa-cve", "vulnerability.cve", `{
			"source": "ghsa",
			"advisory_id": "GHSA-aaaa-bbbb-cccc",
			"cve_id": "CVE-2026-0001",
			"aliases": ["CVE-2026-0001"],
			"cvss_v3": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
			"cvss_v4": "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:H/SI:H/SA:H",
			"cwes": ["CWE-79"],
			"modified_at": "2026-05-21T12:00:00Z"
		}`),
		factRow("osv-cve", "vulnerability.cve", `{
			"source": "osv",
			"advisory_id": "OSV-2026-0001",
			"cve_id": "CVE-2026-0001",
			"aliases": ["GHSA-aaaa-bbbb-cccc"],
			"severity": [{"type": "CVSS_V3", "score": "CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:H/A:H"}],
			"withdrawn_at": "2026-05-24T10:00:00Z"
		}`),
		factRow("nvd-cve", "vulnerability.cve", `{
			"source": "nvd",
			"advisory_id": "CVE-2026-0001",
			"cve_id": "CVE-2026-0001",
			"cvss_score": 9.8,
			"cvss_vector": "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:H/SI:H/SA:H",
			"severity_label": "CRITICAL",
			"cvss_metrics": {"cvss_v40": [{"version": "4.0", "base_score": 9.8}]},
			"cwes": ["CWE-79", "CWE-89"]
		}`),
		factRow("osv-package", "vulnerability.affected_package", `{
			"source": "osv",
			"advisory_id": "OSV-2026-0001",
			"cve_id": "CVE-2026-0001",
			"ecosystem": "npm",
			"package_id": "pkg:npm/example",
			"purl": "pkg:npm/example@1.2.3",
			"affected_ranges": [{"type": "SEMVER", "events": [{"introduced": "0"}, {"fixed": "1.2.4"}]}],
			"fixed_versions": ["1.2.4"]
		}`),
		factRow("glad-package", "vulnerability.affected_package", `{
			"source": "glad",
			"advisory_id": "GHSA-aaaa-bbbb-cccc",
			"cve_id": "CVE-2026-0001",
			"ghsa_id": "GHSA-aaaa-bbbb-cccc",
			"ecosystem": "npm",
			"package_id": "pkg:npm/example",
			"affected_range": "<1.2.5",
			"fixed_versions": ["1.2.5"]
		}`),
		factRow("nvd-product", "vulnerability.affected_product", `{
			"source": "nvd",
			"cve_id": "CVE-2026-0001",
			"criteria": "cpe:2.3:a:example:server:*:*:*:*:*:*:*:*",
			"match_criteria_id": "b5ec4c98-0000-4000-9000-000000000001",
			"vulnerable": true,
			"version_end_excluding": "2.0.0"
		}`),
		factRow("epss", "vulnerability.epss_score", `{
			"source": "first_epss",
			"cve_id": "CVE-2026-0001",
			"probability": "0.71",
			"percentile": "0.98",
			"score_date": "2026-05-24"
		}`),
		factRow("kev", "vulnerability.known_exploited", `{
			"source": "cisa_kev",
			"cve_id": "CVE-2026-0001",
			"date_added": "2026-05-20",
			"required_action": "Apply mitigations",
			"cwes": ["CWE-79"]
		}`),
	}

	got := buildAdvisoryEvidenceRows(rows)
	if len(got) != 1 {
		t.Fatalf("len(rows) = %d, want 1: %#v", len(got), got)
	}
	row := got[0]
	if got, want := row.CanonicalID, "CVE-2026-0001"; got != want {
		t.Fatalf("CanonicalID = %q, want %q", got, want)
	}
	if !stringSliceContains(row.GHSAIDs, "GHSA-aaaa-bbbb-cccc") {
		t.Fatalf("GHSAIDs = %#v, want GHSA id", row.GHSAIDs)
	}
	if !stringSliceContains(row.OSVIDs, "OSV-2026-0001") {
		t.Fatalf("OSVIDs = %#v, want OSV id", row.OSVIDs)
	}
	if !stringSliceContains(row.SourceIDs, "nvd:CVE-2026-0001") {
		t.Fatalf("SourceIDs = %#v, want NVD source identity", row.SourceIDs)
	}
	if len(row.Sources) != 3 {
		t.Fatalf("Sources = %#v, want ghsa/osv/nvd source observations", row.Sources)
	}
	if len(row.EPSS) != 1 || row.EPSS[0].Probability != "0.71" {
		t.Fatalf("EPSS = %#v, want FIRST score", row.EPSS)
	}
	if len(row.KEV) != 1 || row.KEV[0].RequiredAction != "Apply mitigations" {
		t.Fatalf("KEV = %#v, want CISA KEV observation", row.KEV)
	}
	if len(row.AffectedPackages) != 2 {
		t.Fatalf("AffectedPackages = %#v, want OSV and GLAD package rows", row.AffectedPackages)
	}
	if len(row.AffectedProducts) != 1 || row.AffectedProducts[0].Criteria == "" {
		t.Fatalf("AffectedProducts = %#v, want NVD CPE row", row.AffectedProducts)
	}
	if !advisoryEvidenceHasDisagreement(row, "fixed_versions") {
		t.Fatalf("SourceDisagreements = %#v, want fixed_versions disagreement", row.SourceDisagreements)
	}
	if !advisoryEvidenceHasDisagreement(row, "withdrawn_status") {
		t.Fatalf("SourceDisagreements = %#v, want withdrawn_status disagreement", row.SourceDisagreements)
	}
	if !advisoryEvidenceHasDisagreement(row, "severity") {
		t.Fatalf("SourceDisagreements = %#v, want severity disagreement", row.SourceDisagreements)
	}
}

// The typed-decode dead-letter tests (missing-required-field and
// unsupported-schema-major drops) live in
// supply_chain_advisory_evidence_decode_test.go to keep this file under the
// 500-line cap; they share the factRow helper below.

func TestCanonicalAdvisoryKeyNormalizesMixedCaseGHSA(t *testing.T) {
	t.Parallel()

	got := canonicalAdvisoryKey(map[string]any{"advisory_id": "gHsA-aaaa-bbbb-cccc"})
	if want := "GHSA-aaaa-bbbb-cccc"; got != want {
		t.Fatalf("canonicalAdvisoryKey() = %q, want %q", got, want)
	}
}

func TestAdvisoryEvidenceQueryUsesActiveSourceFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = ANY($1::text[])",
		"scope.active_generation_id = fact.generation_id",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->>'cve_id' = lookup.value",
		"fact.payload->>'advisory_id' = lookup.value",
		"fact.payload->>'package_id' = pkg.value",
		"LOWER(fact.payload->>'source') = $4",
		"jsonb_array_elements_text",
		"payload->'correlation_anchors'",
	} {
		if !strings.Contains(listAdvisoryEvidenceQuery, want) {
			t.Fatalf("listAdvisoryEvidenceQuery missing %q:\n%s", want, listAdvisoryEvidenceQuery)
		}
	}
}

func factRow(factID string, factKind string, payload string) advisoryEvidenceFactRow {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		panic(err)
	}
	return advisoryEvidenceFactRow{
		FactID:           factID,
		FactKind:         factKind,
		SourceConfidence: "reported",
		ObservedAt:       "2026-05-24T12:00:00Z",
		Payload:          decoded,
	}
}

func advisoryEvidenceHasDisagreement(row AdvisoryEvidenceRow, field string) bool {
	for _, disagreement := range row.SourceDisagreements {
		if disagreement.Field == field {
			return true
		}
	}
	return false
}
