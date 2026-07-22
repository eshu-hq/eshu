// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// TerraformConfigStateDriftFindingFactKind is the durable reducer fact
// emitted for Terraform config-vs-state drift findings (issue #5442).
const TerraformConfigStateDriftFindingFactKind = "reducer_terraform_config_state_drift_finding"

const (
	terraformConfigStateDriftFindingDefaultLimit = 100
	terraformConfigStateDriftFindingMaxLimit     = 500
)

// TerraformConfigStateDriftFindingFilter bounds active Terraform drift
// finding reads. The store rejects filters without ScopeID and caps list
// pages, mirroring AWSCloudRuntimeDriftFindingFilter's shape.
//
// Scoped and AllowedScopeIDs bind the read to a scoped-token or
// browser-session caller's exact granted repository/ingestion scope, the same
// defense-in-depth double guard AWSCloudRuntimeDriftFindingFilter uses: when
// Scoped is true, ListActiveFindings and CountActiveFindings intersect every
// row with AllowedScopeIDs via `fact.scope_id = ANY(...)`, and return zero
// rows WITHOUT querying Postgres at all when AllowedScopeIDs is empty.
type TerraformConfigStateDriftFindingFilter struct {
	ScopeID    string
	Address    string
	Outcome    string
	DriftKinds []string
	Limit      int
	Offset     int

	Scoped          bool
	AllowedScopeIDs []string
}

// TerraformConfigStateDriftFindingRow is one active reducer finding loaded
// from fact_records.
type TerraformConfigStateDriftFindingRow struct {
	FactID                   string
	ScopeID                  string
	GenerationID             string
	SourceSystem             string
	ObservedAt               time.Time
	CanonicalID              string
	CandidateID              string
	CandidateKind            string
	Outcome                  string
	Address                  string
	DriftKind                string
	BackendKind              string
	LocatorHash              string
	Confidence               float64
	AmbiguousOwnerCandidates []map[string]any
	Evidence                 []TerraformConfigStateDriftEvidenceRow
}

// TerraformConfigStateDriftEvidenceRow preserves the reducer evidence atoms
// used to explain a Terraform config-vs-state drift finding.
type TerraformConfigStateDriftEvidenceRow struct {
	ID           string  `json:"id"`
	SourceSystem string  `json:"source_system"`
	EvidenceType string  `json:"evidence_type"`
	ScopeID      string  `json:"scope_id"`
	Key          string  `json:"key"`
	Value        string  `json:"value"`
	Confidence   float64 `json:"confidence"`
}

// TerraformConfigStateDriftFindingStore reads active Terraform config-vs-state
// drift reducer facts.
type TerraformConfigStateDriftFindingStore struct {
	db ExecQueryer
}

// NewTerraformConfigStateDriftFindingStore constructs a Terraform
// config-vs-state drift finding reader over the provided database adapter.
func NewTerraformConfigStateDriftFindingStore(db ExecQueryer) TerraformConfigStateDriftFindingStore {
	return TerraformConfigStateDriftFindingStore{db: db}
}

// ListActiveFindings returns one page of active Terraform config-vs-state
// drift findings for the caller's bounded scope.
func (s TerraformConfigStateDriftFindingStore) ListActiveFindings(
	ctx context.Context,
	filter TerraformConfigStateDriftFindingFilter,
) ([]TerraformConfigStateDriftFindingRow, error) {
	if s.db == nil {
		return nil, fmt.Errorf("terraform config state drift finding store database is required")
	}
	if filter.Scoped && len(filter.AllowedScopeIDs) == 0 {
		return nil, nil
	}
	filter = normalizeTerraformConfigStateDriftFindingFilter(filter)
	if err := validateTerraformConfigStateDriftFindingFilter(filter); err != nil {
		return nil, err
	}
	query, args := buildTerraformConfigStateDriftFindingQuery(false, filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list active terraform config state drift findings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var findings []TerraformConfigStateDriftFindingRow
	for rows.Next() {
		var row TerraformConfigStateDriftFindingRow
		var payload []byte
		if err := rows.Scan(
			&row.FactID,
			&row.ScopeID,
			&row.GenerationID,
			&row.SourceSystem,
			&row.ObservedAt,
			&payload,
		); err != nil {
			return nil, fmt.Errorf("scan active terraform config state drift finding: %w", err)
		}
		if err := decodeTerraformConfigStateDriftFindingPayload(payload, &row); err != nil {
			return nil, err
		}
		findings = append(findings, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active terraform config state drift findings: %w", err)
	}
	return findings, nil
}

// CountActiveFindings returns the total active finding count for the same
// bounded filters used by ListActiveFindings.
func (s TerraformConfigStateDriftFindingStore) CountActiveFindings(
	ctx context.Context,
	filter TerraformConfigStateDriftFindingFilter,
) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("terraform config state drift finding store database is required")
	}
	if filter.Scoped && len(filter.AllowedScopeIDs) == 0 {
		return 0, nil
	}
	filter = normalizeTerraformConfigStateDriftFindingFilter(filter)
	if err := validateTerraformConfigStateDriftFindingFilter(filter); err != nil {
		return 0, err
	}
	query, args := buildTerraformConfigStateDriftFindingQuery(true, filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("count active terraform config state drift findings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, nil
	}
	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("scan active terraform config state drift finding count: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate active terraform config state drift finding count: %w", err)
	}
	return count, nil
}

type terraformConfigStateDriftFindingPayload struct {
	CanonicalID              string                                 `json:"canonical_id"`
	CandidateID              string                                 `json:"candidate_id"`
	CandidateKind            string                                 `json:"candidate_kind"`
	Outcome                  string                                 `json:"outcome"`
	Address                  string                                 `json:"address"`
	DriftKind                string                                 `json:"drift_kind"`
	BackendKind              string                                 `json:"backend_kind"`
	LocatorHash              string                                 `json:"locator_hash"`
	Confidence               float64                                `json:"confidence"`
	AmbiguousOwnerCandidates []map[string]any                       `json:"ambiguous_owner_candidates"`
	Evidence                 []TerraformConfigStateDriftEvidenceRow `json:"evidence"`
}

func decodeTerraformConfigStateDriftFindingPayload(
	payload []byte,
	row *TerraformConfigStateDriftFindingRow,
) error {
	var decoded terraformConfigStateDriftFindingPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return fmt.Errorf("decode terraform config state drift finding payload: %w", err)
	}
	row.CanonicalID = decoded.CanonicalID
	row.CandidateID = decoded.CandidateID
	row.CandidateKind = decoded.CandidateKind
	row.Outcome = decoded.Outcome
	row.Address = decoded.Address
	row.DriftKind = decoded.DriftKind
	row.BackendKind = decoded.BackendKind
	row.LocatorHash = decoded.LocatorHash
	row.Confidence = decoded.Confidence
	row.AmbiguousOwnerCandidates = append([]map[string]any(nil), decoded.AmbiguousOwnerCandidates...)
	row.Evidence = append([]TerraformConfigStateDriftEvidenceRow(nil), decoded.Evidence...)
	return nil
}

func buildTerraformConfigStateDriftFindingQuery(
	countOnly bool,
	filter TerraformConfigStateDriftFindingFilter,
) (string, []any) {
	selectClause := strings.Join([]string{
		"fact.fact_id",
		"fact.scope_id",
		"fact.generation_id",
		"fact.source_system",
		"fact.observed_at",
		"fact.payload",
	}, ",\n    ")
	if countOnly {
		selectClause = "COUNT(*)"
	}

	args := []any{TerraformConfigStateDriftFindingFactKind}
	conditions := []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = false",
	}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	conditions = append(conditions, "fact.scope_id = "+addArg(filter.ScopeID))
	if filter.Address != "" {
		conditions = append(conditions, "fact.payload->>'address' = "+addArg(filter.Address))
	}
	if filter.Outcome != "" {
		conditions = append(conditions, "fact.payload->>'outcome' = "+addArg(filter.Outcome))
	}
	if len(filter.DriftKinds) > 0 {
		placeholders := make([]string, 0, len(filter.DriftKinds))
		for _, kind := range filter.DriftKinds {
			placeholders = append(placeholders, addArg(kind))
		}
		conditions = append(conditions, "fact.payload->>'drift_kind' IN ("+strings.Join(placeholders, ", ")+")")
	}
	if filter.Scoped {
		// Intersect with the caller's exact granted scopes. ListActiveFindings/
		// CountActiveFindings never reach this builder with Scoped true and an
		// empty AllowedScopeIDs (they short-circuit to zero rows first), but
		// binding `= ANY(allowed_scope_ids)` here even against an empty array
		// is a safe no-op (matches zero rows), not a leak, so this stays
		// unconditional on Scoped rather than also checking length.
		conditions = append(conditions, "fact.scope_id = ANY("+addArg(pq.StringArray(filter.AllowedScopeIDs))+")")
	}

	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, "SELECT\n    %s\n", selectClause)
	builder.WriteString("FROM fact_records AS fact\n")
	builder.WriteString("JOIN ingestion_scopes AS scope\n")
	builder.WriteString("  ON scope.scope_id = fact.scope_id\n")
	builder.WriteString(" AND scope.active_generation_id = fact.generation_id\n")
	builder.WriteString("WHERE ")
	builder.WriteString(strings.Join(conditions, "\n  AND "))
	builder.WriteString("\n")
	if !countOnly {
		limit := addArg(filter.Limit)
		offset := addArg(filter.Offset)
		builder.WriteString("ORDER BY fact.observed_at DESC, fact.fact_id ASC\n")
		_, _ = fmt.Fprintf(&builder, "LIMIT %s OFFSET %s\n", limit, offset)
	}
	return builder.String(), args
}

func normalizeTerraformConfigStateDriftFindingFilter(
	filter TerraformConfigStateDriftFindingFilter,
) TerraformConfigStateDriftFindingFilter {
	filter.ScopeID = strings.TrimSpace(filter.ScopeID)
	filter.Address = strings.TrimSpace(filter.Address)
	filter.Outcome = strings.TrimSpace(filter.Outcome)
	filter.DriftKinds = cleanStringSet(filter.DriftKinds)
	if filter.Limit <= 0 {
		filter.Limit = terraformConfigStateDriftFindingDefaultLimit
	}
	if filter.Limit > terraformConfigStateDriftFindingMaxLimit {
		filter.Limit = terraformConfigStateDriftFindingMaxLimit
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func validateTerraformConfigStateDriftFindingFilter(
	filter TerraformConfigStateDriftFindingFilter,
) error {
	if filter.ScopeID == "" {
		return fmt.Errorf("terraform config state drift finding filter requires scope_id")
	}
	if !strings.HasPrefix(filter.ScopeID, "state_snapshot:") {
		return fmt.Errorf("terraform config state drift finding filter scope_id must be a state_snapshot scope")
	}
	if filter.Outcome != "" && filter.Outcome != "exact" && filter.Outcome != "ambiguous" {
		return fmt.Errorf("terraform config state drift finding filter outcome must be exact or ambiguous")
	}
	return nil
}
