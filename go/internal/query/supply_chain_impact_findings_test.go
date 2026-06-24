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
	"reflect"
	"strings"
	"testing"
)

type recordingSupplyChainImpactFindingStore struct {
	rows       []SupplyChainImpactFindingRow
	lastFilter SupplyChainImpactFindingFilter
}

func (s *recordingSupplyChainImpactFindingStore) ListSupplyChainImpactFindings(
	_ context.Context,
	filter SupplyChainImpactFindingFilter,
) ([]SupplyChainImpactFindingRow, error) {
	s.lastFilter = filter
	return append([]SupplyChainImpactFindingRow(nil), s.rows...), nil
}

type unusedSupplyChainImpactFindingQueryer struct{}

func (unusedSupplyChainImpactFindingQueryer) QueryContext(
	context.Context,
	string,
	...any,
) (*sql.Rows, error) {
	return nil, fmt.Errorf("query must not run for invalid filters")
}

func TestSupplyChainListImpactFindingsRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{ImpactFindings: &recordingSupplyChainImpactFindingStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/impact/findings?limit=10",
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001",
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

func TestPostgresSupplyChainImpactFindingStoreReportsPaginationLimit(t *testing.T) {
	t.Parallel()

	store := NewPostgresSupplyChainImpactFindingStore(unusedSupplyChainImpactFindingQueryer{})

	_, err := store.ListSupplyChainImpactFindings(context.Background(), SupplyChainImpactFindingFilter{
		CVEID: "CVE-2026-0001",
		Limit: supplyChainImpactFindingMaxLimit + 2,
	})
	if err == nil {
		t.Fatal("ListSupplyChainImpactFindings() error = nil, want limit error")
	}
	want := fmt.Sprintf("limit must be between 1 and %d for internal pagination", supplyChainImpactFindingMaxLimit+1)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestPostgresSupplyChainImpactFindingStoreRequiresPositivePriorityScope(t *testing.T) {
	t.Parallel()

	store := NewPostgresSupplyChainImpactFindingStore(unusedSupplyChainImpactFindingQueryer{})

	_, err := store.ListSupplyChainImpactFindings(context.Background(), SupplyChainImpactFindingFilter{
		MinPriorityScore: 0,
		Limit:            10,
	})
	if err == nil {
		t.Fatal("ListSupplyChainImpactFindings() error = nil, want scope error")
	}
	if !strings.Contains(err.Error(), "min_priority_score > 0") {
		t.Fatalf("error = %q, want min_priority_score > 0 scope guidance", err.Error())
	}
}

func TestSupplyChainListImpactFindingsUsesBoundedStore(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{
			{
				FindingID:           "finding-1",
				CVEID:               "CVE-2026-0001",
				PackageID:           "pkg:npm/example",
				PURL:                "pkg:npm/example@1.2.3",
				ProductCriteria:     "cpe:2.3:a:example:server:1.4.2:*:*:*:*:*:*:*",
				MatchCriteriaID:     "b5ec4c98-0000-4000-9000-000000000001",
				ImpactStatus:        "affected_exact",
				Confidence:          "exact",
				CVSSScore:           9.8,
				EPSSProbability:     "0.71",
				KnownExploited:      true,
				RuntimeReachability: "package_manifest",
				RepositoryID:        "repo://example/api",
				DeploymentIDs:       []string{"deployment:example-api"},
				DependencyPath:      []string{"vite", "rollup", "example"},
				DependencyDepth:     3,
				DirectDependency:    boolPtr(false),
				MissingEvidence:     []string{"deployment evidence missing", "service/workload catalog anchor missing"},
				EvidencePath:        []string{"vulnerability.cve", "vulnerability.affected_package", "package_registry.package_version", "reducer_service_catalog_correlation"},
				EvidenceFactIDs:     []string{"cve-1", "affected-1", "version-1", "catalog-1"},
				SourceFreshness:     "active",
				SourceConfidence:    "inferred",
			},
			{FindingID: "finding-2", ImpactStatus: "possibly_affected"},
		},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.CVEID, "CVE-2026-0001"; got != want {
		t.Fatalf("CVEID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}

	var resp struct {
		Findings   []SupplyChainImpactFindingResult `json:"findings"`
		Count      int                              `json:"count"`
		Limit      int                              `json:"limit"`
		Truncated  bool                             `json:"truncated"`
		NextCursor map[string]string                `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d", got, want)
	}
	if !resp.Findings[0].KnownExploited {
		t.Fatalf("KnownExploited = false, want true")
	}
	if got, want := resp.Findings[0].ProductCriteria, "cpe:2.3:a:example:server:1.4.2:*:*:*:*:*:*:*"; got != want {
		t.Fatalf("ProductCriteria = %q, want %q", got, want)
	}
	if got, want := resp.Findings[0].MatchCriteriaID, "b5ec4c98-0000-4000-9000-000000000001"; got != want {
		t.Fatalf("MatchCriteriaID = %q, want %q", got, want)
	}
	if got, want := resp.Findings[0].DependencyPath, []string{"vite", "rollup", "example"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DependencyPath = %#v, want %#v", got, want)
	}
	if got, want := resp.Findings[0].DependencyDepth, 3; got != want {
		t.Fatalf("DependencyDepth = %d, want %d", got, want)
	}
	if resp.Findings[0].DirectDependency == nil || *resp.Findings[0].DirectDependency {
		t.Fatalf("DirectDependency = %#v, want false", resp.Findings[0].DirectDependency)
	}
	if got, want := resp.Findings[0].DeploymentIDs, []string{"deployment:example-api"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DeploymentIDs = %#v, want %#v", got, want)
	}
	if got, want := resp.Findings[0].MissingEvidence, []string{"deployment evidence missing", "service/workload catalog anchor missing"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("MissingEvidence = %#v, want %#v", got, want)
	}
	if got, want := resp.Findings[0].EvidencePath, []string{"vulnerability.cve", "vulnerability.affected_package", "package_registry.package_version", "reducer_service_catalog_correlation"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("EvidencePath = %#v, want %#v", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_finding_id"], "finding-1"; got != want {
		t.Fatalf("next_cursor.after_finding_id = %q, want %q", got, want)
	}
}

func TestSupplyChainListImpactFindingsDoesNotReportPresentCatalogCorrelationAsMissing(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{{
			FindingID:    "finding-catalog-present",
			PackageID:    "pkg:npm/example",
			ImpactStatus: "affected_exact",
			RepositoryID: "repo://example/api",
			WorkloadIDs:  []string{"workload:example-api"},
			EvidencePath: []string{"content_entity", "reducer_service_catalog_correlation"},
			EvidenceFactIDs: []string{
				"content-1",
				"reducer_service_catalog_correlation:catalog-1",
			},
			MissingEvidence: []string{
				"environment evidence missing",
				"service catalog correlation evidence missing",
			},
		}},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/api&limit=10",
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
	if got, want := len(resp.Findings), 1; got != want {
		t.Fatalf("len(findings) = %d, want %d", got, want)
	}
	missing := resp.Findings[0].MissingEvidence
	if containsString(missing, "service catalog correlation evidence missing") {
		t.Fatalf("MissingEvidence = %#v, must not claim present catalog correlation is missing", missing)
	}
	if !containsString(missing, serviceCatalogAnchorMissingReason) {
		t.Fatalf("MissingEvidence = %#v, want %s", missing, serviceCatalogAnchorMissingReason)
	}
}

func TestSupplyChainListImpactFindingsUsesImageRefScope(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?image_ref=registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.ImageRef, "registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; got != want {
		t.Fatalf("ImageRef = %q, want %q", got, want)
	}
}

func TestSupplyChainImpactFindingQueryUsesActiveFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"scope.active_generation_id = fact.generation_id",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->>'cve_id' = $2",
		"fact.payload->>'package_id' = $3",
		"fact.payload->>'repository_id' = $4",
		"fact.payload->>'subject_digest' = $5",
		"fact.payload->>'impact_status' = $6",
		"fact.payload->>'image_ref' = $16",
	} {
		if !strings.Contains(listSupplyChainImpactFindingsQuery, want) {
			t.Fatalf("listSupplyChainImpactFindingsQuery missing %q:\n%s", want, listSupplyChainImpactFindingsQuery)
		}
	}
}

func TestSupplyChainImpactFindingQueryUsesCanonicalFindingRows(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"canonical_key",
		"canonical_facts AS",
		"PARTITION BY canonical_key",
		"payload->>'finding_id'",
		"has_payload_finding_id",
	} {
		if !strings.Contains(listSupplyChainImpactFindingsQuery, want) {
			t.Fatalf("listSupplyChainImpactFindingsQuery missing canonical dedupe marker %q:\n%s", want, listSupplyChainImpactFindingsQuery)
		}
	}
}

func TestSupplyChainImpactCanonicalFindingKeySupportsRollingUpgrades(t *testing.T) {
	t.Parallel()

	if strings.Contains(supplyChainImpactCanonicalFindingKeySQL, "finding_id") {
		t.Fatalf("canonical partition key must not depend on payload finding_id:\n%s", supplyChainImpactCanonicalFindingKeySQL)
	}
	if strings.Contains(listSupplyChainImpactFindingsQuery, "COALESCE(NULLIF(fact.payload->>'finding_id', ''), fact.fact_id) AS finding_id") {
		t.Fatalf("list query must not expose raw fact_id as legacy finding_id fallback:\n%s", listSupplyChainImpactFindingsQuery)
	}
	for _, want := range []string{
		"NULLIF(fact.payload->>'finding_id', '')",
		") AS finding_id",
		"ORDER BY priority_score DESC, has_payload_finding_id DESC, fact_id ASC",
	} {
		if !strings.Contains(listSupplyChainImpactFindingsQuery, want) {
			t.Fatalf("list query missing rolling-upgrade canonical finding marker %q:\n%s", want, listSupplyChainImpactFindingsQuery)
		}
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func TestDecodeSupplyChainImpactFindingRowPreservesCatalogAnchors(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"cve_id": "CVE-2026-7788",
		"package_id": "pkg:npm/example",
		"impact_status": "affected_exact",
		"catalog_entity_refs": ["api:default/example-api"],
		"catalog_owner_refs": ["team:default/platform"],
		"missing_evidence": ["service/workload catalog anchor missing"]
	}`)

	row, err := decodeSupplyChainImpactFindingRow("finding-1", "inferred", payload)
	if err != nil {
		t.Fatalf("decodeSupplyChainImpactFindingRow() error = %v", err)
	}
	if got, want := row.CatalogEntityRefs, []string{"api:default/example-api"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CatalogEntityRefs = %#v, want %#v", got, want)
	}
	if got, want := row.CatalogOwnerRefs, []string{"team:default/platform"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CatalogOwnerRefs = %#v, want %#v", got, want)
	}
	if got, want := row.MissingEvidence, []string{"service/workload catalog anchor missing"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("MissingEvidence = %#v, want %#v", got, want)
	}
}

func TestDecodeSupplyChainImpactFindingRowPreservesProvenance(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
            "cve_id": "CVE-2026-7777",
            "advisory_id": "GHSA-test-1",
            "package_id": "npm://registry.npmjs.org/parse-server",
            "impact_status": "affected_exact",
            "cvss_score": 9.8,
            "requested_range": "^8.6.0",
            "match_reason": "npm_semver_affected_range",
            "fixed_version": "8.6.77",
            "provenance": {
                "selected_severity_source": "ghsa",
                "selected_severity_score": 9.8,
                "selected_severity_vector": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
                "selected_severity_label": "CRITICAL",
                "selected_fixed_version_source": "ghsa",
                "selected_range_source": "ghsa",
                "alternate_severities": [
                    {"source": "nvd", "score": 5.5, "vector": "CVSS:3.1/AV:N/AC:L/PR:L/UI:R/S:U/C:L/I:L/A:N", "label": "MEDIUM"}
                ],
                "fixed_version_branches": [
                    {"version": "8.6.77", "source": "ghsa"},
                    {"version": "9.9.1-alpha.1", "source": "glad"}
                ],
                "advisory_sources": [
                    {"source": "ghsa", "advisory_id": "GHSA-test-1", "source_updated_at": "2026-05-20T12:00:00Z"},
                    {"source": "nvd", "advisory_id": "CVE-2026-7777", "source_updated_at": "2026-05-18T09:00:00Z"},
                    {"source": "glad", "advisory_id": "GMS-2026-99", "source_updated_at": "2026-05-24T08:00:00Z", "withdrawn_at": "2026-05-24T10:00:00Z"}
                ]
            }
        }`)

	row, err := decodeSupplyChainImpactFindingRow("finding-1", "inferred", payload)
	if err != nil {
		t.Fatalf("decodeSupplyChainImpactFindingRow() error = %v", err)
	}
	if row.Provenance == nil {
		t.Fatal("Provenance = nil, want decoded provenance block")
	}
	if row.RequestedRange != "^8.6.0" {
		t.Fatalf("RequestedRange = %q, want ^8.6.0", row.RequestedRange)
	}
	if row.MatchReason != "npm_semver_affected_range" {
		t.Fatalf("MatchReason = %q, want npm_semver_affected_range", row.MatchReason)
	}
	provenance := *row.Provenance
	if provenance.SelectedSeveritySource != "ghsa" {
		t.Fatalf("SelectedSeveritySource = %q, want ghsa", provenance.SelectedSeveritySource)
	}
	if provenance.SelectedFixedVersionSource != "ghsa" {
		t.Fatalf("SelectedFixedVersionSource = %q, want ghsa", provenance.SelectedFixedVersionSource)
	}
	if len(provenance.AlternateSeverities) != 1 || provenance.AlternateSeverities[0].Source != "nvd" {
		t.Fatalf("AlternateSeverities = %#v, want one nvd entry", provenance.AlternateSeverities)
	}
	if len(provenance.FixedVersionBranches) != 2 {
		t.Fatalf("FixedVersionBranches = %#v, want 2 branches", provenance.FixedVersionBranches)
	}
	if len(provenance.AdvisorySources) != 3 {
		t.Fatalf("AdvisorySources = %#v, want 3 sources", provenance.AdvisorySources)
	}
	withdrawnFound := false
	for _, source := range provenance.AdvisorySources {
		if source.Source == "glad" && source.WithdrawnAt == "2026-05-24T10:00:00Z" {
			withdrawnFound = true
		}
	}
	if !withdrawnFound {
		t.Fatalf("AdvisorySources missing withdrawn glad row: %#v", provenance.AdvisorySources)
	}
}
