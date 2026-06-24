// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MultiCloudRuntimeDriftFindingFactKind is the durable reducer fact emitted for
// provider-neutral runtime drift findings (issues #1997, #1998). It mirrors the
// AWS drift fact but keys on canonical cloud_resource_uid so AWS, GCP, and Azure
// findings share one read surface.
const MultiCloudRuntimeDriftFindingFactKind = "reducer_multi_cloud_runtime_drift_finding"

const (
	multiCloudRuntimeDriftFindingDefaultLimit = 100
	multiCloudRuntimeDriftFindingMaxLimit     = 500
)

// MultiCloudRuntimeDriftFindingFilter bounds active multi-cloud drift finding
// reads. The store rejects filters without ScopeID and caps list pages so an
// unbounded provider scan cannot reach the read surface.
type MultiCloudRuntimeDriftFindingFilter struct {
	ScopeID          string
	Provider         string
	CloudResourceUID string
	FindingKinds     []string
	Limit            int
	Offset           int
}

// MultiCloudRuntimeDriftFindingRow is one active reducer finding loaded from
// fact_records, decoded from the provider-neutral drift payload.
type MultiCloudRuntimeDriftFindingRow struct {
	FactID                       string
	ScopeID                      string
	GenerationID                 string
	SourceSystem                 string
	ObservedAt                   time.Time
	CanonicalID                  string
	CandidateID                  string
	CandidateKind                string
	Provider                     string
	CloudResourceUID             string
	RawIdentity                  string
	FindingKind                  string
	ManagementStatus             string
	Confidence                   float64
	MatchedTerraformStateAddress string
	MissingEvidence              []string
	WarningFlags                 []string
	RecommendedAction            string
	Evidence                     []MultiCloudRuntimeDriftEvidenceRow
}

// MultiCloudRuntimeDriftEvidenceRow preserves the reducer evidence atoms used to
// explain one provider-neutral management finding.
type MultiCloudRuntimeDriftEvidenceRow struct {
	ID           string  `json:"id"`
	SourceSystem string  `json:"source_system"`
	EvidenceType string  `json:"evidence_type"`
	ScopeID      string  `json:"scope_id"`
	Key          string  `json:"key"`
	Value        string  `json:"value"`
	Confidence   float64 `json:"confidence"`
}

// MultiCloudRuntimeDriftFindingStore reads active multi-cloud runtime drift
// reducer facts for the unmanaged-resource and runtime-drift query surfaces.
type MultiCloudRuntimeDriftFindingStore struct {
	db ExecQueryer
}

// NewMultiCloudRuntimeDriftFindingStore constructs a multi-cloud runtime drift
// finding reader over the provided database adapter.
func NewMultiCloudRuntimeDriftFindingStore(db ExecQueryer) MultiCloudRuntimeDriftFindingStore {
	return MultiCloudRuntimeDriftFindingStore{db: db}
}

// ListActiveFindings returns one page of active multi-cloud runtime drift
// findings for the caller's bounded scope.
func (s MultiCloudRuntimeDriftFindingStore) ListActiveFindings(
	ctx context.Context,
	filter MultiCloudRuntimeDriftFindingFilter,
) ([]MultiCloudRuntimeDriftFindingRow, error) {
	if s.db == nil {
		return nil, fmt.Errorf("multi cloud runtime drift finding store database is required")
	}
	filter = normalizeMultiCloudRuntimeDriftFindingFilter(filter)
	if err := validateMultiCloudRuntimeDriftFindingFilter(filter); err != nil {
		return nil, err
	}
	query, args := buildMultiCloudRuntimeDriftFindingQuery(false, filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list active multi cloud runtime drift findings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var findings []MultiCloudRuntimeDriftFindingRow
	for rows.Next() {
		var row MultiCloudRuntimeDriftFindingRow
		var payload []byte
		if err := rows.Scan(
			&row.FactID,
			&row.ScopeID,
			&row.GenerationID,
			&row.SourceSystem,
			&row.ObservedAt,
			&payload,
		); err != nil {
			return nil, fmt.Errorf("scan active multi cloud runtime drift finding: %w", err)
		}
		if err := decodeMultiCloudRuntimeDriftFindingPayload(payload, &row); err != nil {
			return nil, err
		}
		findings = append(findings, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active multi cloud runtime drift findings: %w", err)
	}
	return findings, nil
}

// CountActiveFindings returns the total active finding count for the same bounded
// filters used by ListActiveFindings.
func (s MultiCloudRuntimeDriftFindingStore) CountActiveFindings(
	ctx context.Context,
	filter MultiCloudRuntimeDriftFindingFilter,
) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("multi cloud runtime drift finding store database is required")
	}
	filter = normalizeMultiCloudRuntimeDriftFindingFilter(filter)
	if err := validateMultiCloudRuntimeDriftFindingFilter(filter); err != nil {
		return 0, err
	}
	query, args := buildMultiCloudRuntimeDriftFindingQuery(true, filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("count active multi cloud runtime drift findings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, nil
	}
	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("scan active multi cloud runtime drift finding count: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate active multi cloud runtime drift finding count: %w", err)
	}
	return count, nil
}

type multiCloudRuntimeDriftFindingPayload struct {
	CanonicalID                  string                              `json:"canonical_id"`
	CandidateID                  string                              `json:"candidate_id"`
	CandidateKind                string                              `json:"candidate_kind"`
	Provider                     string                              `json:"provider"`
	CloudResourceUID             string                              `json:"cloud_resource_uid"`
	RawIdentity                  string                              `json:"raw_identity"`
	FindingKind                  string                              `json:"finding_kind"`
	ManagementStatus             string                              `json:"management_status"`
	Confidence                   float64                             `json:"confidence"`
	MatchedTerraformStateAddress string                              `json:"matched_terraform_state_address"`
	MissingEvidence              []string                            `json:"missing_evidence"`
	WarningFlags                 []string                            `json:"warning_flags"`
	RecommendedAction            string                              `json:"recommended_action"`
	Evidence                     []MultiCloudRuntimeDriftEvidenceRow `json:"evidence"`
}

func decodeMultiCloudRuntimeDriftFindingPayload(
	payload []byte,
	row *MultiCloudRuntimeDriftFindingRow,
) error {
	var decoded multiCloudRuntimeDriftFindingPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return fmt.Errorf("decode multi cloud runtime drift finding payload: %w", err)
	}
	row.CanonicalID = decoded.CanonicalID
	row.CandidateID = decoded.CandidateID
	row.CandidateKind = decoded.CandidateKind
	row.Provider = decoded.Provider
	row.CloudResourceUID = decoded.CloudResourceUID
	row.RawIdentity = decoded.RawIdentity
	row.FindingKind = decoded.FindingKind
	row.ManagementStatus = decoded.ManagementStatus
	row.Confidence = decoded.Confidence
	row.MatchedTerraformStateAddress = decoded.MatchedTerraformStateAddress
	row.MissingEvidence = append([]string(nil), decoded.MissingEvidence...)
	row.WarningFlags = append([]string(nil), decoded.WarningFlags...)
	row.RecommendedAction = decoded.RecommendedAction
	row.Evidence = append([]MultiCloudRuntimeDriftEvidenceRow(nil), decoded.Evidence...)
	return nil
}

func buildMultiCloudRuntimeDriftFindingQuery(
	countOnly bool,
	filter MultiCloudRuntimeDriftFindingFilter,
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

	args := []any{MultiCloudRuntimeDriftFindingFactKind}
	conditions := []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = false",
	}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	conditions = append(conditions, "fact.scope_id = "+addArg(filter.ScopeID))
	if filter.Provider != "" {
		conditions = append(conditions, "fact.payload->>'provider' = "+addArg(filter.Provider))
	}
	if filter.CloudResourceUID != "" {
		conditions = append(conditions, "fact.payload->>'cloud_resource_uid' = "+addArg(filter.CloudResourceUID))
	}
	if len(filter.FindingKinds) > 0 {
		placeholders := make([]string, 0, len(filter.FindingKinds))
		for _, kind := range filter.FindingKinds {
			placeholders = append(placeholders, addArg(kind))
		}
		conditions = append(conditions, "fact.payload->>'finding_kind' IN ("+strings.Join(placeholders, ", ")+")")
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

func normalizeMultiCloudRuntimeDriftFindingFilter(
	filter MultiCloudRuntimeDriftFindingFilter,
) MultiCloudRuntimeDriftFindingFilter {
	filter.ScopeID = strings.TrimSpace(filter.ScopeID)
	filter.Provider = strings.ToLower(strings.TrimSpace(filter.Provider))
	filter.CloudResourceUID = strings.TrimSpace(filter.CloudResourceUID)
	filter.FindingKinds = cleanStringSet(filter.FindingKinds)
	if filter.Limit <= 0 {
		filter.Limit = multiCloudRuntimeDriftFindingDefaultLimit
	}
	if filter.Limit > multiCloudRuntimeDriftFindingMaxLimit {
		filter.Limit = multiCloudRuntimeDriftFindingMaxLimit
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func validateMultiCloudRuntimeDriftFindingFilter(
	filter MultiCloudRuntimeDriftFindingFilter,
) error {
	if filter.ScopeID == "" {
		return fmt.Errorf("multi cloud runtime drift finding filter requires scope_id")
	}
	return nil
}
