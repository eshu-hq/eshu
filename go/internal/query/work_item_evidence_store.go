// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lib/pq"
)

type workItemEvidenceQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresWorkItemEvidenceStore reads active work-item source facts from
// Postgres.
type PostgresWorkItemEvidenceStore struct {
	DB workItemEvidenceQueryer
}

// NewPostgresWorkItemEvidenceStore creates a Postgres-backed work-item
// evidence store.
func NewPostgresWorkItemEvidenceStore(db workItemEvidenceQueryer) PostgresWorkItemEvidenceStore {
	return PostgresWorkItemEvidenceStore{DB: db}
}

// ListWorkItemEvidence returns one bounded page of active work-item source
// evidence.
func (s PostgresWorkItemEvidenceStore) ListWorkItemEvidence(
	ctx context.Context,
	filter WorkItemEvidenceFilter,
) ([]WorkItemEvidenceRow, error) {
	filter = normalizeWorkItemEvidenceFilter(filter)
	if s.DB == nil {
		return nil, fmt.Errorf("work-item evidence database is required")
	}
	if !filter.hasScope() {
		return nil, fmt.Errorf("scope_id, project_key, work_item_key, provider_work_item_id, url_fingerprint, or observed_after is required")
	}
	if filter.Limit <= 0 || filter.Limit > workItemEvidenceMaxLimit+1 {
		return nil, fmt.Errorf("limit must be between 1 and %d for internal pagination", workItemEvidenceMaxLimit+1)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listWorkItemEvidenceQuery,
		pq.Array(workItemEvidenceFactKinds),
		filter.ScopeID,
		filter.WorkItemKey,
		filter.ProviderWorkItemID,
		filter.ProjectKey,
		filter.URLFingerprint,
		nullableWorkItemEvidenceTime(filter.ObservedAfter),
		filter.AfterFactID,
		pq.Array(filter.AllowedRepositoryIDs),
		filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list work-item evidence: %w", err)
	}
	defer func() { _ = rows.Close() }()

	facts := make([]workItemEvidenceFactRow, 0, filter.Limit)
	for rows.Next() {
		var factID string
		var factKind string
		var scopeID string
		var generationID string
		var sourceConfidence string
		var observedAt sql.NullTime
		var schemaVersion string
		var payloadBytes []byte
		if err := rows.Scan(
			&factID,
			&factKind,
			&scopeID,
			&generationID,
			&sourceConfidence,
			&observedAt,
			&schemaVersion,
			&payloadBytes,
		); err != nil {
			return nil, fmt.Errorf("scan work-item evidence: %w", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil, fmt.Errorf("decode work-item evidence payload: %w", err)
		}
		facts = append(facts, workItemEvidenceFactRow{
			FactID:           factID,
			FactKind:         factKind,
			ScopeID:          scopeID,
			GenerationID:     generationID,
			SourceConfidence: sourceConfidence,
			ObservedAt:       formatNullTime(observedAt),
			SchemaVersion:    schemaVersion,
			Payload:          payload,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list work-item evidence: %w", err)
	}
	return buildWorkItemEvidenceRows(facts), nil
}

func nullableWorkItemEvidenceTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
