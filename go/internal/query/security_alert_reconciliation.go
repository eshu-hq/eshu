package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	Provider              string
	PackageID             string
	CVEID                 string
	GHSAID                string
	ProviderState         string
	ReconciliationStatus  string
	AfterReconciliationID string
	Limit                 int
}

// ProviderSecurityAlertRow is the provider-reported alert state preserved by
// the reconciliation read model.
type ProviderSecurityAlertRow struct {
	Provider            string              `json:"provider,omitempty"`
	ProviderAlertID     string              `json:"provider_alert_id,omitempty"`
	ProviderAlertNumber int64               `json:"provider_alert_number,omitempty"`
	ProviderState       string              `json:"provider_state,omitempty"`
	RepositoryID        string              `json:"repository_id,omitempty"`
	PackageID           string              `json:"package_id,omitempty"`
	Ecosystem           string              `json:"ecosystem,omitempty"`
	PackageName         string              `json:"package_name,omitempty"`
	ManifestPath        string              `json:"manifest_path,omitempty"`
	DependencyScope     string              `json:"dependency_scope,omitempty"`
	Relationship        string              `json:"relationship,omitempty"`
	GHSAIDs             []string            `json:"ghsa_ids,omitempty"`
	CVEIDs              []string            `json:"cve_ids,omitempty"`
	VulnerableRange     string              `json:"vulnerable_range,omitempty"`
	PatchedVersion      string              `json:"patched_version,omitempty"`
	Severity            string              `json:"severity,omitempty"`
	CVSS                map[string]any      `json:"cvss,omitempty"`
	EPSS                map[string]string   `json:"epss,omitempty"`
	CWEs                []map[string]string `json:"cwes,omitempty"`
	Summary             string              `json:"summary,omitempty"`
	SourceURL           string              `json:"source_url,omitempty"`
	CreatedAt           string              `json:"created_at,omitempty"`
	UpdatedAt           string              `json:"updated_at,omitempty"`
	FixedAt             string              `json:"fixed_at,omitempty"`
	DismissedAt         string              `json:"dismissed_at,omitempty"`
}

// SecurityAlertEshuImpactRow carries Eshu-owned impact state matched to a
// provider alert. Empty fields mean no Eshu impact finding was admitted.
type SecurityAlertEshuImpactRow struct {
	ImpactStatus string `json:"impact_status,omitempty"`
	FindingID    string `json:"finding_id,omitempty"`
}

// SecurityAlertReconciliationRow is one reducer-owned comparison row.
type SecurityAlertReconciliationRow struct {
	ReconciliationID     string                     `json:"reconciliation_id"`
	ProviderAlert        ProviderSecurityAlertRow   `json:"provider_alert"`
	EshuImpact           SecurityAlertEshuImpactRow `json:"eshu_impact"`
	ReconciliationStatus string                     `json:"reconciliation_status"`
	Reason               string                     `json:"reason,omitempty"`
	EvidenceFactIDs      []string                   `json:"evidence_fact_ids,omitempty"`
	SourceFreshness      string                     `json:"source_freshness,omitempty"`
	SourceConfidence     string                     `json:"source_confidence,omitempty"`
}

// SecurityAlertReconciliationResult is the public API row shape.
type SecurityAlertReconciliationResult = SecurityAlertReconciliationRow

type securityAlertReconciliationQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

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
		filter.RepositoryID,
		filter.Provider,
		filter.PackageID,
		filter.CVEID,
		filter.GHSAID,
		filter.ProviderState,
		filter.ReconciliationStatus,
		filter.AfterReconciliationID,
		filter.Limit,
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

const listSecurityAlertReconciliationsQuery = `
SELECT fact.fact_id, fact.source_confidence, fact.payload
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
  AND ($2 = '' OR fact.payload->>'repository_id' = $2)
  AND ($3 = '' OR fact.payload->>'provider' = $3)
  AND ($4 = '' OR fact.payload->>'package_id' = $4)
  AND ($5 = '' OR fact.payload->'cve_ids' ? $5)
  AND ($6 = '' OR fact.payload->'ghsa_ids' ? $6)
  AND ($7 = '' OR fact.payload->>'provider_state' = $7)
  AND ($8 = '' OR fact.payload->>'reconciliation_status' = $8)
  AND ($9 = '' OR fact.fact_id > $9)
ORDER BY fact.fact_id ASC
LIMIT $10
`

func (f SecurityAlertReconciliationFilter) hasScope() bool {
	return f.RepositoryID != "" || f.Provider != "" || f.PackageID != "" ||
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
		},
		EshuImpact: SecurityAlertEshuImpactRow{
			ImpactStatus: StringVal(payload, "eshu_impact_status"),
			FindingID:    StringVal(payload, "eshu_impact_finding_id"),
		},
		ReconciliationStatus: StringVal(payload, "reconciliation_status"),
		Reason:               StringVal(payload, "reason"),
		EvidenceFactIDs:      StringSliceVal(payload, "evidence_fact_ids"),
		SourceFreshness:      "active",
		SourceConfidence:     sourceConfidence,
	}, nil
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
