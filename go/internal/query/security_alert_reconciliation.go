// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

const (
	securityAlertReconciliationFactKind              = "reducer_security_alert_reconciliation"
	securityAlertReconciliationAnchorRequiredMessage = "repository_id, provider, package_id, cve_id, or ghsa_id is required; provider_state and reconciliation_status are filters only"
)

// SecurityAlertReconciliationStore reads reducer-owned provider alert
// reconciliation rows.
type SecurityAlertReconciliationStore interface {
	ListSecurityAlertReconciliations(
		context.Context,
		SecurityAlertReconciliationFilter,
	) ([]SecurityAlertReconciliationRow, error)
}

// SecurityAlertReconciliationFilter bounds provider alert reconciliation reads
// to a repository, provider, package, or advisory id anchor. Provider state and
// reconciliation status narrow anchored pages but are not standalone scopes.
type SecurityAlertReconciliationFilter struct {
	RepositoryID          string
	RepositoryScopeIDs    []string
	Provider              string
	PackageID             string
	CVEID                 string
	GHSAID                string
	ProviderState         string
	ReconciliationStatus  string
	AfterReconciliationID string
	Limit                 int
	// AllowedSourceRepositoryIDs carries the scoped-token grant set (union of
	// granted repository and ingestion-scope ids). When populated, reconciliation
	// facts are intersected with the grant set (matching repository_id,
	// provider_repository_id, or scope_id) before deduplication, ordering,
	// limits, cursors, and count metadata. Empty means unrestricted
	// (shared-token, all-scope admin, or local dev).
	AllowedSourceRepositoryIDs []string
}

// ProviderSecurityAlertRow is the provider-reported alert state preserved by
// the reconciliation read model.
type ProviderSecurityAlertRow struct {
	Provider                    string              `json:"provider,omitempty"`
	ProviderAlertID             string              `json:"provider_alert_id,omitempty"`
	ProviderAlertNumber         int64               `json:"provider_alert_number,omitempty"`
	ProviderState               string              `json:"provider_state,omitempty"`
	RepositoryID                string              `json:"repository_id,omitempty"`
	PackageID                   string              `json:"package_id,omitempty"`
	Ecosystem                   string              `json:"ecosystem,omitempty"`
	PackageName                 string              `json:"package_name,omitempty"`
	ManifestPath                string              `json:"manifest_path,omitempty"`
	DependencyScope             string              `json:"dependency_scope,omitempty"`
	Relationship                string              `json:"relationship,omitempty"`
	GHSAIDs                     []string            `json:"ghsa_ids,omitempty"`
	CVEIDs                      []string            `json:"cve_ids,omitempty"`
	VulnerableRange             string              `json:"vulnerable_range,omitempty"`
	PatchedVersion              string              `json:"patched_version,omitempty"`
	Severity                    string              `json:"severity,omitempty"`
	CVSS                        map[string]any      `json:"cvss,omitempty"`
	EPSS                        map[string]string   `json:"epss,omitempty"`
	CWEs                        []map[string]string `json:"cwes,omitempty"`
	Summary                     string              `json:"summary,omitempty"`
	SourceURL                   string              `json:"source_url,omitempty"`
	CreatedAt                   string              `json:"created_at,omitempty"`
	UpdatedAt                   string              `json:"updated_at,omitempty"`
	FixedAt                     string              `json:"fixed_at,omitempty"`
	DismissedAt                 string              `json:"dismissed_at,omitempty"`
	CollectionCoverageState     string              `json:"collection_coverage_state,omitempty"`
	CollectionTruncated         bool                `json:"collection_truncated,omitempty"`
	CollectionPagesFetched      int64               `json:"collection_pages_fetched,omitempty"`
	CollectionStateFilter       string              `json:"collection_state_filter,omitempty"`
	CollectionIncompleteReasons []string            `json:"collection_incomplete_reasons,omitempty"`
}

// SecurityAlertEshuImpactRow carries Eshu-owned impact state matched to a
// provider alert. Empty fields mean no Eshu impact finding was admitted.
type SecurityAlertEshuImpactRow struct {
	ImpactStatus string `json:"impact_status,omitempty"`
	FindingID    string `json:"finding_id,omitempty"`
}

// SecurityAlertEshuPackageRow carries Eshu-owned dependency evidence matched to
// a provider alert. ObservedVersion is never copied from provider alert fields.
type SecurityAlertEshuPackageRow struct {
	ObservedVersion        string   `json:"observed_version,omitempty"`
	RequestedRange         string   `json:"requested_range,omitempty"`
	DependencyRange        string   `json:"dependency_range,omitempty"`
	DependencyEvidenceID   string   `json:"dependency_evidence_id,omitempty"`
	DependencyEvidenceKind string   `json:"dependency_evidence_kind,omitempty"`
	MissingEvidence        []string `json:"missing_evidence,omitempty"`
}

// SecurityAlertReconciliationRow is one reducer-owned comparison row.
type SecurityAlertReconciliationRow struct {
	ReconciliationID     string                         `json:"reconciliation_id"`
	ProviderAlert        ProviderSecurityAlertRow       `json:"provider_alert"`
	EshuPackage          SecurityAlertEshuPackageRow    `json:"eshu_package"`
	EshuImpact           SecurityAlertEshuImpactRow     `json:"eshu_impact"`
	ReconciliationStatus string                         `json:"reconciliation_status"`
	Reason               string                         `json:"reason,omitempty"`
	ReasonCode           string                         `json:"reason_code,omitempty"`
	MissingEvidence      []SecurityAlertMissingEvidence `json:"missing_evidence,omitempty"`
	EvidenceFactIDs      []string                       `json:"evidence_fact_ids,omitempty"`
	SourceFreshness      string                         `json:"source_freshness,omitempty"`
	SourceConfidence     string                         `json:"source_confidence,omitempty"`
}

// SecurityAlertReconciliationResult is the public API row shape.
type SecurityAlertReconciliationResult = SecurityAlertReconciliationRow

type securityAlertReconciliationQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

const securityAlertProviderRepositoryScopesQuery = `
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

// PostgresSecurityAlertReconciliationStore reads active provider alert
// reconciliation facts from Postgres.
type PostgresSecurityAlertReconciliationStore struct {
	DB securityAlertReconciliationQueryer
}

// NewPostgresSecurityAlertReconciliationStore creates the Postgres-backed
// provider alert reconciliation read model.
func NewPostgresSecurityAlertReconciliationStore(
	db securityAlertReconciliationQueryer,
) PostgresSecurityAlertReconciliationStore {
	return PostgresSecurityAlertReconciliationStore{DB: db}
}

// ListSecurityAlertReconciliations returns one bounded page of active provider
// alert reconciliation rows.
func (s PostgresSecurityAlertReconciliationStore) ListSecurityAlertReconciliations(
	ctx context.Context,
	filter SecurityAlertReconciliationFilter,
) ([]SecurityAlertReconciliationRow, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("security alert reconciliation database is required")
	}
	if !filter.hasScope() {
		return nil, errors.New(securityAlertReconciliationAnchorRequiredMessage)
	}
	if filter.Limit <= 0 || filter.Limit > securityAlertReconciliationMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", securityAlertReconciliationMaxLimit+1)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listSecurityAlertReconciliationsQuery,
		securityAlertReconciliationFactKind,
		pq.Array(securityAlertRepositoryScopeIDs(filter.RepositoryID, filter.RepositoryScopeIDs)),
		filter.Provider,
		filter.PackageID,
		filter.CVEID,
		filter.GHSAID,
		filter.ProviderState,
		filter.ReconciliationStatus,
		filter.AfterReconciliationID,
		filter.Limit,
		pq.Array(filter.AllowedSourceRepositoryIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("list security alert reconciliations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]SecurityAlertReconciliationRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var sourceConfidence string
		var payloadBytes []byte
		if err := rows.Scan(&factID, &sourceConfidence, &payloadBytes); err != nil {
			return nil, fmt.Errorf("list security alert reconciliations: %w", err)
		}
		row, err := decodeSecurityAlertReconciliationRow(factID, sourceConfidence, payloadBytes)
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
// ambiguous rather than guessing the provider owner.
func (s PostgresSecurityAlertReconciliationStore) SecurityAlertProviderRepositoryScopes(
	ctx context.Context,
	repositoryName string,
) ([]string, error) {
	if s.DB == nil {
		return nil, fmt.Errorf("security alert reconciliation database is required")
	}
	return securityAlertProviderRepositoryScopes(ctx, s.DB, repositoryName)
}

func securityAlertProviderRepositoryScopes(
	ctx context.Context,
	db securityAlertReconciliationQueryer,
	repositoryName string,
) ([]string, error) {
	repositoryName = strings.TrimSpace(repositoryName)
	if repositoryName == "" {
		return nil, nil
	}
	rows, err := db.QueryContext(
		ctx,
		securityAlertProviderRepositoryScopesQuery,
		securityAlertReconciliationFactKind,
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

func (f SecurityAlertReconciliationFilter) hasScope() bool {
	return f.RepositoryID != "" || len(f.RepositoryScopeIDs) > 0 ||
		f.Provider != "" || f.PackageID != "" ||
		f.CVEID != "" || f.GHSAID != ""
}

func decodeSecurityAlertReconciliationRow(
	factID string,
	sourceConfidence string,
	payloadBytes []byte,
) (SecurityAlertReconciliationRow, error) {
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return SecurityAlertReconciliationRow{}, fmt.Errorf("decode security alert reconciliation: %w", err)
	}
	return SecurityAlertReconciliationRow{
		ReconciliationID: factID,
		ProviderAlert: ProviderSecurityAlertRow{
			Provider:            StringVal(payload, "provider"),
			ProviderAlertID:     StringVal(payload, "provider_alert_id"),
			ProviderAlertNumber: int64(floatVal(payload, "provider_alert_number")),
			ProviderState:       StringVal(payload, "provider_state"),
			RepositoryID:        StringVal(payload, "repository_id"),
			PackageID:           StringVal(payload, "package_id"),
			Ecosystem:           StringVal(payload, "ecosystem"),
			PackageName:         StringVal(payload, "package_name"),
			ManifestPath:        StringVal(payload, "manifest_path"),
			DependencyScope:     StringVal(payload, "dependency_scope"),
			Relationship:        StringVal(payload, "relationship"),
			GHSAIDs:             StringSliceVal(payload, "ghsa_ids"),
			CVEIDs:              StringSliceVal(payload, "cve_ids"),
			VulnerableRange:     StringVal(payload, "vulnerable_range"),
			PatchedVersion:      StringVal(payload, "patched_version"),
			Severity:            StringVal(payload, "severity"),
			CVSS:                mapVal(payload, "cvss"),
			EPSS:                stringMapVal(payload, "epss"),
			CWEs:                stringMapSliceVal(payload, "cwes"),
			Summary:             StringVal(payload, "summary"),
			SourceURL:           StringVal(payload, "source_url"),
			CreatedAt:           StringVal(payload, "created_at"),
			UpdatedAt:           StringVal(payload, "updated_at"),
			FixedAt:             StringVal(payload, "fixed_at"),
			DismissedAt:         StringVal(payload, "dismissed_at"),
			CollectionCoverageState: StringVal(
				payload,
				"collection_coverage_state",
			),
			CollectionTruncated:         BoolVal(payload, "collection_truncated"),
			CollectionPagesFetched:      int64(floatVal(payload, "collection_pages_fetched")),
			CollectionStateFilter:       StringVal(payload, "collection_state_filter"),
			CollectionIncompleteReasons: StringSliceVal(payload, "collection_incomplete_reasons"),
		},
		EshuImpact: SecurityAlertEshuImpactRow{
			ImpactStatus: StringVal(payload, "eshu_impact_status"),
			FindingID:    StringVal(payload, "eshu_impact_finding_id"),
		},
		EshuPackage: SecurityAlertEshuPackageRow{
			ObservedVersion:        StringVal(payload, "observed_version"),
			RequestedRange:         StringVal(payload, "requested_range"),
			DependencyRange:        StringVal(payload, "dependency_range"),
			DependencyEvidenceID:   StringVal(payload, "dependency_evidence_id"),
			DependencyEvidenceKind: StringVal(payload, "dependency_evidence_kind"),
			MissingEvidence: securityAlertPackageMissingEvidence(
				StringSliceVal(payload, "package_missing_evidence"),
				StringSliceVal(payload, "missing_evidence"),
			),
		},
		ReconciliationStatus: StringVal(payload, "reconciliation_status"),
		Reason:               StringVal(payload, "reason"),
		ReasonCode:           StringVal(payload, "reason_code"),
		MissingEvidence:      securityAlertMissingEvidenceVal(payload, "missing_evidence"),
		EvidenceFactIDs:      StringSliceVal(payload, "evidence_fact_ids"),
		SourceFreshness:      securityAlertSourceFreshness(payload),
		SourceConfidence:     sourceConfidence,
	}, nil
}

func securityAlertPackageMissingEvidence(
	current []string,
	legacy []string,
) []string {
	if len(current) > 0 {
		return current
	}
	return legacy
}

func securityAlertSourceFreshness(payload map[string]any) string {
	if freshness := StringVal(payload, "source_freshness"); freshness != "" {
		return freshness
	}
	if StringVal(payload, "collection_coverage_state") == "incomplete" {
		return "partial"
	}
	return "active"
}

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

func stringMapVal(payload map[string]any, key string) map[string]string {
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
