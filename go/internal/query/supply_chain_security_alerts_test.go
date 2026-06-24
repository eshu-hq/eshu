// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type recordingSecurityAlertReconciliationStore struct {
	rows       []SecurityAlertReconciliationRow
	lastFilter SecurityAlertReconciliationFilter
}

type panicSecurityAlertReconciliationDB struct{}

func (panicSecurityAlertReconciliationDB) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	panic("security alert reconciliation query should not execute")
}

func (s *recordingSecurityAlertReconciliationStore) ListSecurityAlertReconciliations(
	_ context.Context,
	filter SecurityAlertReconciliationFilter,
) ([]SecurityAlertReconciliationRow, error) {
	s.lastFilter = filter
	return append([]SecurityAlertReconciliationRow(nil), s.rows...), nil
}

func TestSupplyChainListSecurityAlertReconciliationsRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{SecurityAlerts: &recordingSecurityAlertReconciliationStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/security-alerts/reconciliations?limit=10",
		"/api/v0/supply-chain/security-alerts/reconciliations?provider_state=open&limit=10",
		"/api/v0/supply-chain/security-alerts/reconciliations?reconciliation_status=matched&limit=10",
		"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=repo://github/eshu-hq/eshu",
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

func TestPostgresSecurityAlertReconciliationRejectsFilterOnlyStateOrStatus(t *testing.T) {
	t.Parallel()

	store := PostgresSecurityAlertReconciliationStore{DB: panicSecurityAlertReconciliationDB{}}
	for name, filter := range map[string]SecurityAlertReconciliationFilter{
		"provider_state":        {ProviderState: "open", Limit: 1},
		"reconciliation_status": {ReconciliationStatus: "matched", Limit: 1},
		"state_and_status":      {ProviderState: "open", ReconciliationStatus: "matched", Limit: 1},
	} {
		filter := filter
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := store.ListSecurityAlertReconciliations(context.Background(), filter)
			if err == nil {
				t.Fatal("ListSecurityAlertReconciliations error = nil, want unanchored filter error")
			}
			if !strings.Contains(err.Error(), "repository_id, provider, package_id, cve_id, or ghsa_id is required") {
				t.Fatalf("error = %q, want selective anchor requirement", err)
			}
		})
	}
}

func TestSupplyChainListSecurityAlertReconciliationsSeparatesProviderAndEshuState(t *testing.T) {
	t.Parallel()

	store := &recordingSecurityAlertReconciliationStore{
		rows: []SecurityAlertReconciliationRow{
			{
				ReconciliationID: "reconciliation-1",
				ProviderAlert: ProviderSecurityAlertRow{
					Provider:            "github_dependabot",
					ProviderAlertNumber: 42,
					ProviderState:       "open",
					RepositoryID:        "repo://github/eshu-hq/eshu",
					PackageID:           "npm://registry.npmjs.org/left-pad",
					PackageName:         "left-pad",
					Ecosystem:           "npm",
					ManifestPath:        "package-lock.json",
					DependencyScope:     "runtime",
					Relationship:        "direct",
					GHSAIDs:             []string{"GHSA-abcd-1234"},
					CVEIDs:              []string{"CVE-2026-0001"},
					VulnerableRange:     "<1.2.3",
					PatchedVersion:      "1.2.3",
					Severity:            "critical",
					SourceURL:           "https://github.com/eshu-hq/eshu/security/dependabot/42",
				},
				EshuImpact: SecurityAlertEshuImpactRow{
					ImpactStatus: "affected_exact",
					FindingID:    "impact-1",
				},
				EshuPackage: SecurityAlertEshuPackageRow{
					ObservedVersion:        "1.2.0",
					RequestedRange:         "^1.0.0",
					DependencyRange:        "1.2.0",
					DependencyEvidenceID:   "consume-1",
					DependencyEvidenceKind: "reducer_package_consumption_correlation",
				},
				ReconciliationStatus: "matched",
				Reason:               "provider alert matches owned dependency and reducer impact evidence",
				EvidenceFactIDs:      []string{"alert-1", "consume-1", "impact-1"},
				SourceFreshness:      "active",
				SourceConfidence:     "inferred",
			},
			{ReconciliationID: "reconciliation-2", ReconciliationStatus: "provider_only"},
		},
	}
	handler := &SupplyChainHandler{SecurityAlerts: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=repo://github/eshu-hq/eshu&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.RepositoryID, "repo://github/eshu-hq/eshu"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}

	var resp struct {
		Reconciliations []SecurityAlertReconciliationResult `json:"reconciliations"`
		Count           int                                 `json:"count"`
		Limit           int                                 `json:"limit"`
		Truncated       bool                                `json:"truncated"`
		NextCursor      map[string]string                   `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Reconciliations), 1; got != want {
		t.Fatalf("len(reconciliations) = %d, want %d", got, want)
	}
	row := resp.Reconciliations[0]
	if got, want := row.ProviderAlert.ProviderState, "open"; got != want {
		t.Fatalf("ProviderAlert.ProviderState = %q, want %q", got, want)
	}
	if got, want := row.EshuImpact.ImpactStatus, "affected_exact"; got != want {
		t.Fatalf("EshuImpact.ImpactStatus = %q, want %q", got, want)
	}
	if got, want := row.EshuPackage.ObservedVersion, "1.2.0"; got != want {
		t.Fatalf("EshuPackage.ObservedVersion = %q, want %q", got, want)
	}
	if got, want := row.ReconciliationStatus, "matched"; got != want {
		t.Fatalf("ReconciliationStatus = %q, want %q", got, want)
	}
	if got, want := row.ProviderAlert.GHSAIDs, []string{"GHSA-abcd-1234"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ProviderAlert.GHSAIDs = %#v, want %#v", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_reconciliation_id"], "reconciliation-1"; got != want {
		t.Fatalf("next_cursor.after_reconciliation_id = %q, want %q", got, want)
	}
}

func TestSupplyChainListSecurityAlertReconciliationsSurfacesIncompleteProviderCoverage(t *testing.T) {
	t.Parallel()

	store := &recordingSecurityAlertReconciliationStore{
		rows: []SecurityAlertReconciliationRow{{
			ReconciliationID: "reconciliation-partial",
			ProviderAlert: ProviderSecurityAlertRow{
				Provider:            "github_dependabot",
				ProviderAlertNumber: 17,
				ProviderState:       "open",
				RepositoryID:        "repo://github/example-org/example-repo",
				PackageID:           "npm://registry.npmjs.org/left-pad",
			},
			ReconciliationStatus: "provider_only",
			SourceFreshness:      "partial",
			SourceConfidence:     "inferred",
		}},
	}
	handler := &SupplyChainHandler{SecurityAlerts: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/security-alerts/reconciliations?repository_id=repo://github/example-org/example-repo&provider_state=open&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Count           int `json:"count"`
		Reconciliations []struct {
			SourceFreshness string `json:"source_freshness"`
		} `json:"reconciliations"`
		Coverage struct {
			State          string `json:"state"`
			PartialRows    int    `json:"partial_rows"`
			RowsConsidered int    `json:"rows_considered"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.Count, 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if got, want := resp.Reconciliations[0].SourceFreshness, "partial"; got != want {
		t.Fatalf("source_freshness = %q, want %q", got, want)
	}
	if got, want := resp.Coverage.State, "target_incomplete"; got != want {
		t.Fatalf("coverage.state = %q, want %q", got, want)
	}
	if got, want := resp.Coverage.PartialRows, 1; got != want {
		t.Fatalf("coverage.partial_rows = %d, want %d", got, want)
	}
	if got, want := resp.Coverage.RowsConsidered, 1; got != want {
		t.Fatalf("coverage.rows_considered = %d, want %d", got, want)
	}
}

func TestDecodeSecurityAlertReconciliationRowPreservesProviderCoverage(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"provider":                      "github_dependabot",
		"provider_alert_number":         float64(17),
		"provider_state":                "open",
		"repository_id":                 "repo://github/example-org/example-repo",
		"package_id":                    "npm://registry.npmjs.org/left-pad",
		"reconciliation_status":         "provider_only",
		"source_freshness":              "partial",
		"collection_coverage_state":     "incomplete",
		"collection_truncated":          true,
		"collection_pages_fetched":      float64(2),
		"collection_state_filter":       "open",
		"collection_incomplete_reasons": []any{"provider_open_alert_page_limit_reached"},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	row, err := decodeSecurityAlertReconciliationRow("reconciliation-partial", "inferred", raw)
	if err != nil {
		t.Fatalf("decodeSecurityAlertReconciliationRow() error = %v, want nil", err)
	}
	if got, want := row.SourceFreshness, "partial"; got != want {
		t.Fatalf("SourceFreshness = %q, want %q", got, want)
	}
	if got, want := row.ProviderAlert.CollectionCoverageState, "incomplete"; got != want {
		t.Fatalf("CollectionCoverageState = %q, want %q", got, want)
	}
	if !row.ProviderAlert.CollectionTruncated {
		t.Fatal("CollectionTruncated = false, want true")
	}
	if got, want := row.ProviderAlert.CollectionPagesFetched, int64(2); got != want {
		t.Fatalf("CollectionPagesFetched = %d, want %d", got, want)
	}
	if got, want := row.ProviderAlert.CollectionStateFilter, "open"; got != want {
		t.Fatalf("CollectionStateFilter = %q, want %q", got, want)
	}
	if got, want := row.ProviderAlert.CollectionIncompleteReasons, []string{"provider_open_alert_page_limit_reached"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CollectionIncompleteReasons = %#v, want %#v", got, want)
	}
}

func TestDecodeSecurityAlertReconciliationRowPreservesOwnedPackageEvidence(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"provider":                  "github_dependabot",
		"provider_alert_number":     float64(42),
		"provider_state":            "open",
		"repository_id":             "repo://github/example-org/example-repo",
		"package_id":                "npm://registry.npmjs.org/left-pad",
		"observed_version":          "1.2.0",
		"requested_range":           "^1.0.0",
		"dependency_range":          "1.2.0",
		"dependency_evidence_id":    "consume-1",
		"dependency_evidence_kind":  "reducer_package_consumption_correlation",
		"package_missing_evidence":  []any{"installed package version malformed"},
		"reconciliation_status":     "matched",
		"eshu_impact_status":        "affected_exact",
		"eshu_impact_finding_id":    "impact-1",
		"provider_observed_version": "provider-value-must-not-be-used",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	row, err := decodeSecurityAlertReconciliationRow("reconciliation-1", "inferred", raw)
	if err != nil {
		t.Fatalf("decodeSecurityAlertReconciliationRow() error = %v, want nil", err)
	}
	if got, want := row.EshuPackage.ObservedVersion, "1.2.0"; got != want {
		t.Fatalf("EshuPackage.ObservedVersion = %q, want %q", got, want)
	}
	if got, want := row.EshuPackage.RequestedRange, "^1.0.0"; got != want {
		t.Fatalf("EshuPackage.RequestedRange = %q, want %q", got, want)
	}
	if got, want := row.EshuPackage.DependencyRange, "1.2.0"; got != want {
		t.Fatalf("EshuPackage.DependencyRange = %q, want %q", got, want)
	}
	if got, want := row.EshuPackage.DependencyEvidenceID, "consume-1"; got != want {
		t.Fatalf("EshuPackage.DependencyEvidenceID = %q, want %q", got, want)
	}
	if got, want := row.EshuPackage.DependencyEvidenceKind, "reducer_package_consumption_correlation"; got != want {
		t.Fatalf("EshuPackage.DependencyEvidenceKind = %q, want %q", got, want)
	}
	if got, want := row.EshuPackage.MissingEvidence, []string{"installed package version malformed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("EshuPackage.MissingEvidence = %#v, want %#v", got, want)
	}
}

func TestDecodeSecurityAlertReconciliationRowPreservesLegacyOwnedPackageMissingEvidence(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"package_id":            "npm://registry.npmjs.org/left-pad",
		"missing_evidence":      []any{"installed package version malformed"},
		"reconciliation_status": "unmatched",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	row, err := decodeSecurityAlertReconciliationRow("reconciliation-legacy", "inferred", raw)
	if err != nil {
		t.Fatalf("decodeSecurityAlertReconciliationRow() error = %v, want nil", err)
	}
	if got, want := row.EshuPackage.MissingEvidence, []string{"installed package version malformed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("EshuPackage.MissingEvidence = %#v, want %#v", got, want)
	}
}

func TestPostgresSecurityAlertReconciliationQueryShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"ROW_NUMBER() OVER (",
		"PARTITION BY",
		"security_alert_current_rank",
		"COALESCE(NULLIF(fact.payload->>'provider_alert_id', ''),",
		"COALESCE(NULLIF(fact.payload->>'provider_repository_id', ''),",
		"COALESCE(NULLIF(fact.payload->'cve_ids', 'null'::jsonb), '[]'::jsonb)",
		"COALESCE(NULLIF(fact.payload->'ghsa_ids', 'null'::jsonb), '[]'::jsonb)",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"fact.payload->>'repository_id' = ANY($2::text[])",
		"fact.payload->>'provider_repository_id' = ANY($2::text[])",
		"fact.payload->>'scope_id' = ANY($2::text[])",
		"fact.payload->>'provider' = $3",
		"fact.payload->>'package_id' = $4",
		"fact.payload->'cve_ids' ? $5",
		"fact.payload->'ghsa_ids' ? $6",
		"LIMIT $10",
	} {
		if !strings.Contains(listSecurityAlertReconciliationsQuery, want) {
			t.Fatalf("listSecurityAlertReconciliationsQuery missing %q:\n%s", want, listSecurityAlertReconciliationsQuery)
		}
	}
	currentRank := strings.Index(listSecurityAlertReconciliationsQuery, "security_alert_current_rank = 1")
	if currentRank < 0 {
		t.Fatalf("listSecurityAlertReconciliationsQuery missing current-rank filter:\n%s", listSecurityAlertReconciliationsQuery)
	}
	for _, filter := range []string{
		"current_fact.payload->>'provider_state' = $7",
		"current_fact.payload->>'reconciliation_status' = $8",
		"current_fact.fact_id > $9",
	} {
		filterIndex := strings.Index(listSecurityAlertReconciliationsQuery, filter)
		if filterIndex < currentRank {
			t.Fatalf("filter %q must apply after current-rank selection:\n%s", filter, listSecurityAlertReconciliationsQuery)
		}
	}
}

func TestSecurityAlertProviderRepositoryScopesQueryIsExactAndBounded(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"provider_scope LIKE 'security-alert:%/%'",
		"REGEXP_REPLACE(provider_scope, '^security-alert:[^:]+:.*/', '')",
		"LOWER($2)",
		"LIMIT 2",
	} {
		if !strings.Contains(securityAlertProviderRepositoryScopesQuery, want) {
			t.Fatalf("securityAlertProviderRepositoryScopesQuery missing %q:\n%s", want, securityAlertProviderRepositoryScopesQuery)
		}
	}
}
