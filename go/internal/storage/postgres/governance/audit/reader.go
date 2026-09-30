// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auditstore

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// GovernanceAuditReader reads authorized audit events and safe aggregates.
// It exposes no append, schema, or retention mutation methods.
type GovernanceAuditReader struct {
	database db.Queryer
	// Logger receives bounded unknown-enum warnings; nil uses slog.Default.
	Logger *slog.Logger
}

// NewGovernanceAuditReader accepts a query-only store, including guarded replicas.
func NewGovernanceAuditReader(database db.Queryer) GovernanceAuditReader {
	return GovernanceAuditReader{database: database}
}

// WithLogger returns a copy whose unknown-enum warnings use logger.
func (s GovernanceAuditReader) WithLogger(logger *slog.Logger) GovernanceAuditReader {
	s.Logger = logger
	return s
}

func (s GovernanceAuditReader) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// List returns private detailed events for an explicitly authorized operator
// query.
func (s GovernanceAuditReader) List(ctx context.Context, filter GovernanceAuditQuery) ([]governanceaudit.Event, error) {
	if !filter.OperatorAuthorized {
		return nil, ErrGovernanceAuditQueryUnauthorized
	}
	if s.database == nil {
		return nil, fmt.Errorf("governance audit store db is required")
	}
	query, args := buildGovernanceAuditListQuery(filter)
	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query governance audit events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	events := []governanceaudit.Event{}
	var unknown governanceAuditUnknownTally
	for rows.Next() {
		event, err := scanGovernanceAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		unknown.add(event)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate governance audit events: %w", err)
	}
	// One warn per field per page, so an operator can see that this pod is
	// listing rows a newer build wrote (#6574) without a line per row.
	unknown.warn(ctx, s.logger())
	return events, nil
}

// Summary returns aggregate counts that are safe for status and MCP readbacks.
func (s GovernanceAuditReader) Summary(ctx context.Context) (governanceaudit.Summary, error) {
	if s.database == nil {
		return governanceaudit.Summary{}, fmt.Errorf("governance audit store db is required")
	}
	rows, err := s.database.QueryContext(ctx, governanceAuditSummarySQL)
	if err != nil {
		return governanceaudit.Summary{}, fmt.Errorf("summarize governance audit events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var summary governanceaudit.Summary
	for rows.Next() {
		var category, name string
		var count int64
		var lastOccurredAt time.Time
		if err := rows.Scan(&category, &name, &count, &lastOccurredAt); err != nil {
			return governanceaudit.Summary{}, fmt.Errorf("scan governance audit summary: %w", err)
		}
		applyGovernanceAuditSummaryRow(&summary, category, name, int(count), lastOccurredAt)
	}
	if err := rows.Err(); err != nil {
		return governanceaudit.Summary{}, fmt.Errorf("iterate governance audit summary: %w", err)
	}
	return summary, nil
}

// SummaryForTenant returns aggregate counts scoped to a single tenant. Global
// (NULL-tenant) events are excluded — only events with a matching tenant_id are
// counted. The shared operator should use Summary instead.
func (s GovernanceAuditReader) SummaryForTenant(ctx context.Context, tenantID string) (governanceaudit.Summary, error) {
	if s.database == nil {
		return governanceaudit.Summary{}, fmt.Errorf("governance audit store db is required")
	}
	const sqlTemplate = `
WITH base AS (
    SELECT event_type, actor_class, scope_class, decision, reason_code, occurred_at
    FROM governance_audit_events
    WHERE tenant_id = $1
),
summary_rows AS (
    SELECT 'total' AS category, '' AS name, COUNT(*)::BIGINT AS count,
        COALESCE(MAX(occurred_at), '1970-01-01T00:00:00Z'::timestamptz) AS last_occurred_at
    FROM base
    UNION ALL
    SELECT 'event_type', event_type, COUNT(*)::BIGINT, MAX(occurred_at)
    FROM base GROUP BY event_type
    UNION ALL
    SELECT 'decision', decision, COUNT(*)::BIGINT, MAX(occurred_at)
    FROM base GROUP BY decision
    UNION ALL
    SELECT 'reason', reason_code, COUNT(*)::BIGINT, MAX(occurred_at)
    FROM base GROUP BY reason_code
    UNION ALL
    SELECT 'actor_class', actor_class, COUNT(*)::BIGINT, MAX(occurred_at)
    FROM base GROUP BY actor_class
    UNION ALL
    SELECT 'scope_class', scope_class, COUNT(*)::BIGINT, MAX(occurred_at)
    FROM base GROUP BY scope_class
)
SELECT category, name, count, last_occurred_at
FROM summary_rows
ORDER BY category ASC, name ASC
`
	rows, err := s.database.QueryContext(ctx, sqlTemplate, tenantID)
	if err != nil {
		return governanceaudit.Summary{}, fmt.Errorf("summarize governance audit events for tenant: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var summary governanceaudit.Summary
	for rows.Next() {
		var category, name string
		var count int64
		var lastOccurredAt time.Time
		if err := rows.Scan(&category, &name, &count, &lastOccurredAt); err != nil {
			return governanceaudit.Summary{}, fmt.Errorf("scan governance audit tenant summary: %w", err)
		}
		applyGovernanceAuditSummaryRow(&summary, category, name, int(count), lastOccurredAt)
	}
	if err := rows.Err(); err != nil {
		return governanceaudit.Summary{}, fmt.Errorf("iterate governance audit tenant summary: %w", err)
	}
	return summary, nil
}
