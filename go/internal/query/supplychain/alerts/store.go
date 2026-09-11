// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alerts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// factKind is the reducer fact kind backing the reconciliation read model
// (#6060 lane A move to this leaf). It is a route-serves-data registry
// evidence marker (docs/internal/design/5584-route-serves-data-registry.md);
// keep the value below byte-identical if this file moves homes again.
const factKind = "reducer_security_alert_reconciliation"

// Queryer is the narrow *sql.DB surface PostgresStore needs. Exported
// because it is NewPostgresStore's parameter type: root's
// NewPostgresSecurityAlertReconciliationStore forwarder in
// supply_chain_hub_alias.go, and in turn cmd/api and cmd/mcp-server wiring
// (which pass a *sql.DB), must be able to name it.
type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

const providerRepositoryScopesQuery = `
WITH active_provider_scopes AS (
  SELECT DISTINCT COALESCE(
      NULLIF(fact.payload->>'provider_repository_id', ''),
      NULLIF(fact.payload->>'scope_id', ''),
      NULLIF(fact.payload->>'repository_id', '')
    ) AS provider_scope
  FROM fact_records AS fact
  JOIN ingestion_scopes AS scope
    ON scope.scope_id = fact.scope_id
   AND scope.active_generation_id = fact.generation_id
  JOIN scope_generations AS generation
    ON generation.scope_id = fact.scope_id
   AND generation.generation_id = fact.generation_id
  WHERE fact.fact_kind = $1
    AND fact.is_tombstone = FALSE
    AND generation.status = 'active'
)
SELECT provider_scope
FROM active_provider_scopes
WHERE provider_scope LIKE 'security-alert:%/%'
  AND LOWER(REGEXP_REPLACE(provider_scope, '^security-alert:[^:]+:.*/', '')) = LOWER($2)
ORDER BY provider_scope ASC
LIMIT 2
`

// PostgresStore reads active provider alert reconciliation facts from
// Postgres.
type PostgresStore struct {
	DB Queryer
}

// PostgresStore satisfies the hub's read port; drift fails here rather than
// at a call site. Moved from root's supply_chain_hub_alias.go pin (#6642).
var _ supplychain.SecurityAlertReconciliationStore = PostgresStore{}

// NewPostgresStore creates the Postgres-backed provider alert reconciliation
// read model.
func NewPostgresStore(db Queryer) PostgresStore {
	return PostgresStore{DB: db}
}

// ListSecurityAlertReconciliations returns one bounded page of active provider
// alert reconciliation rows. The method name matches
// supplychain.SecurityAlertReconciliationStore; it is a port contract, not
// stutter.
func (s PostgresStore) ListSecurityAlertReconciliations(
	ctx context.Context,
	filter supplychain.SecurityAlertReconciliationFilter,
) ([]supplychain.SecurityAlertReconciliationRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("security alert reconciliation database is required")
	}
	if !filter.HasScope() {
		return nil, errors.New(supplychain.SecurityAlertReconciliationAnchorRequiredMessage)
	}
	if filter.Limit <= 0 || filter.Limit > supplychain.SecurityAlertReconciliationMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", supplychain.SecurityAlertReconciliationMaxLimit+1)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listQuery,
		factKind,
		pgarray.Array(supplychain.SecurityAlertRepositoryScopeIDs(filter.RepositoryID, filter.RepositoryScopeIDs)),
		filter.Provider,
		filter.PackageID,
		filter.CVEID,
		filter.GHSAID,
		filter.ProviderState,
		filter.ReconciliationStatus,
		filter.AfterReconciliationID,
		filter.Limit,
		pgarray.Array(filter.AllowedSourceRepositoryIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("list security alert reconciliations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]supplychain.SecurityAlertReconciliationRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var sourceConfidence string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &sourceConfidence, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list security alert reconciliations: %w", err)
		}
		row, err := decodeRow(factID, sourceConfidence, payloadBytes)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list security alert reconciliations: %w", err)
	}
	return out, nil
}

// SecurityAlertProviderRepositoryScopes returns provider-owned security alert
// repository scopes whose exact repository-name segment matches the supplied
// source repository name. Callers must treat multiple returned scopes as
// ambiguous rather than guessing the provider owner. The method name matches
// supplychain.SecurityAlertReconciliationStore's sibling caller contract; it
// is not stutter.
func (s PostgresStore) SecurityAlertProviderRepositoryScopes(
	ctx context.Context,
	repositoryName string,
) ([]string, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("security alert reconciliation database is required")
	}
	return providerRepositoryScopes(ctx, s.DB, repositoryName)
}

// providerRepositoryScopes shares the provider-owned repository-scope lookup
// between PostgresStore and PostgresAggregateStore.
func providerRepositoryScopes(
	ctx context.Context,
	db Queryer,
	repositoryName string,
) ([]string, error) {
	repositoryName = strings.TrimSpace(repositoryName)
	if repositoryName == "" {
		return nil, nil
	}
	rows, err := db.QueryContext(
		ctx,
		providerRepositoryScopesQuery,
		factKind,
		repositoryName,
	)
	if err != nil {
		return nil, fmt.Errorf("list provider security alert repository scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []string{}
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			return nil, fmt.Errorf("scan provider security alert repository scope: %w", err)
		}
		out = append(out, strings.TrimSpace(scope))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider security alert repository scopes: %w", err)
	}
	return out, nil
}

// decodeRow decodes one reconciliation fact row into the public read-model
// shape.
func decodeRow(
	factID string,
	sourceConfidence string,
	payloadBytes []byte,
) (supplychain.SecurityAlertReconciliationRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return supplychain.SecurityAlertReconciliationRow{}, fmt.Errorf("decode security alert reconciliation: %w", err)
	}
	return supplychain.SecurityAlertReconciliationRow{
		ReconciliationID: factID,
		ProviderAlert: supplychain.ProviderSecurityAlertRow{
			Provider:            querycontract.StringVal(payload, "provider"),
			ProviderAlertID:     querycontract.StringVal(payload, "provider_alert_id"),
			ProviderAlertNumber: int64(querycontract.FloatVal(payload, "provider_alert_number")),
			ProviderState:       querycontract.StringVal(payload, "provider_state"),
			RepositoryID:        querycontract.StringVal(payload, "repository_id"),
			PackageID:           querycontract.StringVal(payload, "package_id"),
			Ecosystem:           querycontract.StringVal(payload, "ecosystem"),
			PackageName:         querycontract.StringVal(payload, "package_name"),
			ManifestPath:        querycontract.StringVal(payload, "manifest_path"),
			DependencyScope:     querycontract.StringVal(payload, "dependency_scope"),
			Relationship:        querycontract.StringVal(payload, "relationship"),
			GHSAIDs:             querycontract.StringSliceVal(payload, "ghsa_ids"),
			CVEIDs:              querycontract.StringSliceVal(payload, "cve_ids"),
			VulnerableRange:     querycontract.StringVal(payload, "vulnerable_range"),
			PatchedVersion:      querycontract.StringVal(payload, "patched_version"),
			Severity:            querycontract.StringVal(payload, "severity"),
			CVSS:                mapVal(payload, "cvss"),
			EPSS:                StringMapVal(payload, "epss"),
			CWEs:                stringMapSliceVal(payload, "cwes"),
			Summary:             querycontract.StringVal(payload, "summary"),
			SourceURL:           querycontract.StringVal(payload, "source_url"),
			CreatedAt:           querycontract.StringVal(payload, "created_at"),
			UpdatedAt:           querycontract.StringVal(payload, "updated_at"),
			FixedAt:             querycontract.StringVal(payload, "fixed_at"),
			DismissedAt:         querycontract.StringVal(payload, "dismissed_at"),
			CollectionCoverageState: querycontract.StringVal(
				payload,
				"collection_coverage_state",
			),
			CollectionTruncated:         querycontract.BoolVal(payload, "collection_truncated"),
			CollectionPagesFetched:      int64(querycontract.FloatVal(payload, "collection_pages_fetched")),
			CollectionStateFilter:       querycontract.StringVal(payload, "collection_state_filter"),
			CollectionIncompleteReasons: querycontract.StringSliceVal(payload, "collection_incomplete_reasons"),
		},
		EshuImpact: supplychain.SecurityAlertEshuImpactRow{
			ImpactStatus: querycontract.StringVal(payload, "eshu_impact_status"),
			FindingID:    querycontract.StringVal(payload, "eshu_impact_finding_id"),
		},
		EshuPackage: supplychain.SecurityAlertEshuPackageRow{
			ObservedVersion:        querycontract.StringVal(payload, "observed_version"),
			RequestedRange:         querycontract.StringVal(payload, "requested_range"),
			DependencyRange:        querycontract.StringVal(payload, "dependency_range"),
			DependencyEvidenceID:   querycontract.StringVal(payload, "dependency_evidence_id"),
			DependencyEvidenceKind: querycontract.StringVal(payload, "dependency_evidence_kind"),
			MissingEvidence: packageMissingEvidence(
				querycontract.StringSliceVal(payload, "package_missing_evidence"),
				querycontract.StringSliceVal(payload, "missing_evidence"),
			),
		},
		ReconciliationStatus: querycontract.StringVal(payload, "reconciliation_status"),
		Reason:               querycontract.StringVal(payload, "reason"),
		ReasonCode:           querycontract.StringVal(payload, "reason_code"),
		MissingEvidence:      missingEvidenceVal(payload, "missing_evidence"),
		EvidenceFactIDs:      querycontract.StringSliceVal(payload, "evidence_fact_ids"),
		SourceFreshness:      sourceFreshness(payload),
		SourceConfidence:     sourceConfidence,
	}, nil
}

// packageMissingEvidence prefers the current field name over the legacy one,
// so an older reducer payload still surfaces its missing-evidence detail.
func packageMissingEvidence(
	current []string,
	legacy []string,
) []string {
	if len(current) > 0 {
		return current
	}
	return legacy
}

// sourceFreshness derives the row's freshness label from the payload,
// defaulting an incomplete collection to "partial" and everything else to
// "active".
func sourceFreshness(payload map[string]any) string {
	if freshness := querycontract.StringVal(payload, "source_freshness"); freshness != "" {
		return freshness
	}
	if querycontract.StringVal(payload, "collection_coverage_state") == "incomplete" {
		return "partial"
	}
	return "active"
}

// mapVal extracts a map[string]any payload field, trimming empty keys and nil
// values and returning nil rather than an empty map.
func mapVal(payload map[string]any, key string) map[string]any {
	raw, ok := payload[key].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		if strings.TrimSpace(key) != "" && value != nil {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// StringMapVal extracts a map[string]string payload field, stringifying each
// value and dropping empty keys or values. Exported because root's
// sbom_attestation_attachments.go and sbom_attestation_attachment_rows.go
// call it through an unexported forward in supply_chain_hub_alias.go.
func StringMapVal(payload map[string]any, key string) map[string]string {
	raw, ok := payload[key].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		text := strings.TrimSpace(fmt.Sprint(value))
		if strings.TrimSpace(key) != "" && text != "" && text != "<nil>" {
			out[key] = text
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// stringMapSliceVal extracts a []map[string]string payload field, applying
// the same stringify-and-drop-empty rule as StringMapVal to each element.
func stringMapSliceVal(payload map[string]any, key string) []map[string]string {
	items, ok := payload[key].([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	out := make([]map[string]string, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		row := make(map[string]string, len(raw))
		for key, value := range raw {
			text := strings.TrimSpace(fmt.Sprint(value))
			if strings.TrimSpace(key) != "" && text != "" && text != "<nil>" {
				row[key] = text
			}
		}
		if len(row) > 0 {
			out = append(out, row)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
