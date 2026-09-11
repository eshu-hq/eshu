// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workitem

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/advisory"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

type workItemEvidenceQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresEvidenceStore reads active work-item source facts from Postgres.
//
// Package query keeps this type available as PostgresWorkItemEvidenceStore
// through a type alias in work_item_alias.go (#6642).
type PostgresEvidenceStore struct {
	DB workItemEvidenceQueryer
}

// NewPostgresEvidenceStore creates a Postgres-backed work-item evidence
// store. Package query keeps this constructor available as
// NewPostgresWorkItemEvidenceStore through a forwarder in work_item_alias.go
// (#6642).
func NewPostgresEvidenceStore(db workItemEvidenceQueryer) PostgresEvidenceStore {
	return PostgresEvidenceStore{DB: db}
}

// ListWorkItemEvidence returns one bounded page of active work-item source
// evidence. filter.Limit is the store's "+1" lookahead fetch bound (the
// caller's requested page size plus one); the returned EvidencePage's
// Truncated and NextCursorFactID are derived from how many facts were
// actually fetched, not from how many decoded, so a malformed fact inside the
// visible window can never corrupt pagination (#4733).
func (s PostgresEvidenceStore) ListWorkItemEvidence(
	ctx context.Context,
	filter EvidenceFilter,
) (EvidencePage, error) {
	filter = normalizeWorkItemEvidenceFilter(filter)
	if s.DB == nil {
		return EvidencePage{}, fmt.Errorf("work-item evidence database is required")
	}
	if !filter.hasScope() {
		return EvidencePage{}, fmt.Errorf("scope_id, project_key, work_item_key, provider_work_item_id, url_fingerprint, or observed_after is required")
	}
	if filter.Limit <= 0 || filter.Limit > evidenceMaxLimit+1 {
		return EvidencePage{}, fmt.Errorf("limit must be between 1 and %d for internal pagination", evidenceMaxLimit+1)
	}

	rows, err := s.DB.QueryContext(
		ctx,
		listWorkItemEvidenceQuery,
		pgarray.Array(EvidenceFactKinds),
		filter.ScopeID,
		filter.WorkItemKey,
		filter.ProviderWorkItemID,
		filter.ProjectKey,
		filter.URLFingerprint,
		nullableWorkItemEvidenceTime(filter.ObservedAfter),
		filter.AfterFactID,
		pgarray.Array(filter.AllowedRepositoryIDs),
		filter.Limit,
	)
	if err != nil {
		return EvidencePage{}, fmt.Errorf("list work-item evidence: %w", err)
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
			return EvidencePage{}, fmt.Errorf("scan work-item evidence: %w", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return EvidencePage{}, fmt.Errorf("decode work-item evidence payload: %w", err)
		}
		facts = append(facts, workItemEvidenceFactRow{
			FactID:           factID,
			FactKind:         factKind,
			ScopeID:          scopeID,
			GenerationID:     generationID,
			SourceConfidence: sourceConfidence,
			ObservedAt:       advisory.FormatNullTime(observedAt),
			SchemaVersion:    schemaVersion,
			Payload:          payload,
		})
	}
	if err := rows.Err(); err != nil {
		return EvidencePage{}, fmt.Errorf("list work-item evidence: %w", err)
	}
	return buildWorkItemEvidencePage(facts, filter.Limit), nil
}

func nullableWorkItemEvidenceTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
