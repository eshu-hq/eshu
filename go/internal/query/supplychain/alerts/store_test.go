// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alerts

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
)

type panicSecurityAlertReconciliationDB struct{}

func (panicSecurityAlertReconciliationDB) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	panic("security alert reconciliation query should not execute")
}

func TestPostgresSecurityAlertReconciliationRejectsFilterOnlyStateOrStatus(t *testing.T) {
	t.Parallel()

	store := PostgresStore{DB: panicSecurityAlertReconciliationDB{}}
	for name, filter := range map[string]supplychain.SecurityAlertReconciliationFilter{
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

	row, err := decodeRow("reconciliation-partial", "inferred", raw)
	if err != nil {
		t.Fatalf("decodeRow() error = %v, want nil", err)
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

	row, err := decodeRow("reconciliation-1", "inferred", raw)
	if err != nil {
		t.Fatalf("decodeRow() error = %v, want nil", err)
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

	row, err := decodeRow("reconciliation-legacy", "inferred", raw)
	if err != nil {
		t.Fatalf("decodeRow() error = %v, want nil", err)
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
		"COALESCE(cardinality($2::text[]), 0) = 0",
		"fact.payload->>'repository_id' = ANY($2::text[])",
		"fact.payload->>'provider_repository_id' = ANY($2::text[])",
		"fact.payload->>'scope_id' = ANY($2::text[])",
		"fact.payload->>'provider' = $3",
		"fact.payload->>'package_id' = $4",
		"fact.payload->'cve_ids' ? $5",
		"fact.payload->'ghsa_ids' ? $6",
		"COALESCE(cardinality($11::text[]), 0) = 0",
		"LIMIT $10",
	} {
		if !strings.Contains(listQuery, want) {
			t.Fatalf("listQuery missing %q:\n%s", want, listQuery)
		}
	}
	currentRank := strings.Index(listQuery, "security_alert_current_rank = 1")
	if currentRank < 0 {
		t.Fatalf("listQuery missing current-rank filter:\n%s", listQuery)
	}
	for _, filter := range []string{
		"current_fact.payload->>'provider_state' = $7",
		"current_fact.payload->>'reconciliation_status' = $8",
		"current_fact.fact_id > $9",
	} {
		filterIndex := strings.Index(listQuery, filter)
		if filterIndex < currentRank {
			t.Fatalf("filter %q must apply after current-rank selection:\n%s", filter, listQuery)
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
		if !strings.Contains(providerRepositoryScopesQuery, want) {
			t.Fatalf("providerRepositoryScopesQuery missing %q:\n%s", want, providerRepositoryScopesQuery)
		}
	}
}
