// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/decisions"
)

// NewReadStore constructs the inspection capability over a query-only connection.
func NewReadStore(database db.Queryer) admin.ReadStore {
	if database == nil {
		return nil
	}
	return &postgresReadStore{database: database, decisions: decisionsstore.NewDecisionReader(database)}
}

type postgresReadStore struct {
	database  db.Queryer
	decisions *decisionsstore.DecisionReader
}

// ListWorkItems reads the bounded admin inspection result.
func (s *postgresReadStore) ListWorkItems(ctx context.Context, f admin.WorkItemFilter) ([]admin.WorkItem, error) {
	query, args := buildListWorkItemsQuery(f)
	return scanWorkItems(ctx, s.database, query, args...)
}

// ListReplayEvents reads the bounded admin inspection result.
func (s *postgresReadStore) ListReplayEvents(ctx context.Context, f admin.ReplayEventFilter) ([]admin.ReplayEvent, error) {
	var builder strings.Builder
	builder.WriteString(`
SELECT replay_event_id, work_item_id, scope_id, generation_id, failure_class, operator_note, created_at
FROM fact_replay_events
WHERE 1=1
`)
	args := make([]any, 0, 4)
	if value := strings.TrimSpace(f.ScopeID); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND scope_id = $%d\n", len(args))
	}
	if value := strings.TrimSpace(f.WorkItemID); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND work_item_id = $%d\n", len(args))
	}
	if value := strings.TrimSpace(f.FailureClass); value != "" {
		args = append(args, value)
		_, _ = fmt.Fprintf(&builder, " AND failure_class = $%d\n", len(args))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	args = append(args, limit)
	_, _ = fmt.Fprintf(&builder, " ORDER BY created_at DESC, replay_event_id DESC LIMIT $%d", len(args))

	rows, err := s.database.QueryContext(ctx, builder.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list replay events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []admin.ReplayEvent
	for rows.Next() {
		var event admin.ReplayEvent
		var failureClass sql.NullString
		var operatorNote sql.NullString
		if err := rows.Scan(
			&event.ReplayEventID,
			&event.WorkItemID,
			&event.ScopeID,
			&event.GenerationID,
			&failureClass,
			&operatorNote,
			&event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan replay event: %w", err)
		}
		if failureClass.Valid {
			event.FailureClass = &failureClass.String
		}
		if operatorNote.Valid {
			event.OperatorNote = &operatorNote.String
		}
		events = append(events, event)
	}

	return events, rows.Err()
}

// ListDecisions reads the bounded admin inspection result.
func (s *postgresReadStore) ListDecisions(ctx context.Context, f admin.DecisionQueryFilter) ([]admin.DecisionRow, error) {
	rows, err := s.decisions.ListDecisions(ctx, decisionsstore.DecisionFilter{
		RepositoryID: f.RepositoryID,
		SourceRunID:  f.SourceRunID,
		DecisionType: f.DecisionType,
		Limit:        f.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list decisions: %w", err)
	}

	result := make([]admin.DecisionRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, admin.DecisionRow{
			DecisionID:        row.DecisionID,
			DecisionType:      row.DecisionType,
			RepositoryID:      row.RepositoryID,
			SourceRunID:       row.SourceRunID,
			WorkItemID:        row.WorkItemID,
			Subject:           row.Subject,
			ConfidenceScore:   row.ConfidenceScore,
			ConfidenceReason:  row.ConfidenceReason,
			ProvenanceSummary: row.ProvenanceSummary,
			CreatedAt:         row.CreatedAt,
		})
	}
	return result, nil
}

// ListEvidence reads the bounded admin inspection result.
func (s *postgresReadStore) ListEvidence(ctx context.Context, decisionID string) ([]admin.EvidenceRow, error) {
	rows, err := s.decisions.ListEvidence(ctx, decisionID)
	if err != nil {
		return nil, fmt.Errorf("list evidence: %w", err)
	}

	result := make([]admin.EvidenceRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, admin.EvidenceRow{
			EvidenceID:   row.EvidenceID,
			DecisionID:   row.DecisionID,
			FactID:       row.FactID,
			EvidenceKind: row.EvidenceKind,
			Detail:       row.Detail,
			CreatedAt:    row.CreatedAt,
		})
	}
	return result, nil
}

// ListDeadLetterWorkItems preserves inspection behavior for legacy writer-backed stores.
func (s *postgresStore) ListDeadLetterWorkItems(ctx context.Context, f admin.DeadLetterListFilter) ([]admin.DeadLetterWorkItem, error) {
	return s.reader().ListDeadLetterWorkItems(ctx, f)
}

// ListReducerInputInvalidFacts preserves inspection behavior for legacy writer-backed stores.
func (s *postgresStore) ListReducerInputInvalidFacts(ctx context.Context, f admin.InputInvalidFactListFilter) ([]admin.InputInvalidFact, error) {
	return s.reader().ListReducerInputInvalidFacts(ctx, f)
}

// ListChangedSincePoisonedLinks preserves inspection behavior for legacy writer-backed stores.
func (s *postgresStore) ListChangedSincePoisonedLinks(ctx context.Context, f admin.ChangedSincePoisonedLinkFilter) ([]admin.ChangedSincePoisonedLink, error) {
	return s.reader().ListChangedSincePoisonedLinks(ctx, f)
}
