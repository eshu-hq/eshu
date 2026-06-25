// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

const (
	governanceAuditBatchSize     = 500
	governanceAuditColumnsPerRow = 15
	defaultGovernanceAuditLimit  = 100
	maxGovernanceAuditLimit      = 500
)

// ErrGovernanceAuditQueryUnauthorized marks a detailed audit query without an
// operator authorization gate.
var ErrGovernanceAuditQueryUnauthorized = errors.New("governance audit detailed query is unauthorized")

// GovernanceAuditQuery bounds private detailed audit queries.
type GovernanceAuditQuery struct {
	OperatorAuthorized bool
	EventType          governanceaudit.EventType
	ActorClass         governanceaudit.ActorClass
	ScopeClass         governanceaudit.ScopeClass
	Decision           governanceaudit.Decision
	ReasonCode         string
	CorrelationID      string
	OccurredAfter      time.Time
	OccurredBefore     time.Time
	Limit              int
	// OrderDesc reverses the default ASC ordering to occurred_at DESC so that
	// callers like the admin read path see the most-recent events first under
	// LIMIT rather than the oldest. The default (false) preserves the existing
	// ASC behaviour used by all other callers.
	OrderDesc bool
	// TenantID constrains the query to a single tenant. When non-empty, only
	// events with a matching tenant_id are returned. Global/NULL-tenant events
	// are excluded when TenantID is set (a tenant admin must not see them).
	// Leave empty for shared-operator queries that should see all events.
	TenantID string
}

// GovernanceAuditStore persists normalized hosted governance audit events.
type GovernanceAuditStore struct {
	db ExecQueryer
}

// NewGovernanceAuditStore creates a Postgres-backed governance audit store.
func NewGovernanceAuditStore(db ExecQueryer) GovernanceAuditStore {
	return GovernanceAuditStore{db: db}
}

// EnsureSchema applies the private audit sink DDL.
func (s GovernanceAuditStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("governance audit store db is required")
	}
	_, err := s.db.ExecContext(ctx, governanceAuditEventsSchemaSQL)
	return err
}

// Append validates and persists audit-safe events with retry-idempotent keys.
func (s GovernanceAuditStore) Append(ctx context.Context, events []governanceaudit.Event) error {
	if s.db == nil {
		return fmt.Errorf("governance audit store db is required")
	}
	if len(events) == 0 {
		return nil
	}
	normalized := make([]governanceaudit.Event, 0, len(events))
	for _, event := range events {
		value, err := governanceaudit.NormalizeEvent(event)
		if err != nil {
			return fmt.Errorf("normalize governance audit event: %w", err)
		}
		normalized = append(normalized, value)
	}
	persistedAt := time.Now().UTC()
	for i := 0; i < len(normalized); i += governanceAuditBatchSize {
		end := i + governanceAuditBatchSize
		if end > len(normalized) {
			end = len(normalized)
		}
		if err := s.appendBatch(ctx, normalized[i:end], persistedAt); err != nil {
			return err
		}
	}
	return nil
}

// List returns private detailed events for an explicitly authorized operator
// query.
func (s GovernanceAuditStore) List(ctx context.Context, filter GovernanceAuditQuery) ([]governanceaudit.Event, error) {
	if !filter.OperatorAuthorized {
		return nil, ErrGovernanceAuditQueryUnauthorized
	}
	if s.db == nil {
		return nil, fmt.Errorf("governance audit store db is required")
	}
	query, args := buildGovernanceAuditListQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query governance audit events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	events := []governanceaudit.Event{}
	for rows.Next() {
		event, err := scanGovernanceAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate governance audit events: %w", err)
	}
	return events, nil
}

// Summary returns aggregate counts that are safe for status and MCP readbacks.
func (s GovernanceAuditStore) Summary(ctx context.Context) (governanceaudit.Summary, error) {
	if s.db == nil {
		return governanceaudit.Summary{}, fmt.Errorf("governance audit store db is required")
	}
	rows, err := s.db.QueryContext(ctx, governanceAuditSummarySQL)
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
func (s GovernanceAuditStore) SummaryForTenant(ctx context.Context, tenantID string) (governanceaudit.Summary, error) {
	if s.db == nil {
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
	rows, err := s.db.QueryContext(ctx, sqlTemplate, tenantID)
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

// DeleteExpired removes detailed events older than the hosted retention cutoff.
func (s GovernanceAuditStore) DeleteExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	if s.db == nil {
		return 0, fmt.Errorf("governance audit store db is required")
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM governance_audit_events WHERE occurred_at < $1", cutoff.UTC())
	if err != nil {
		return 0, fmt.Errorf("delete expired governance audit events: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read deleted governance audit count: %w", err)
	}
	return deleted, nil
}

func (s GovernanceAuditStore) appendBatch(
	ctx context.Context,
	events []governanceaudit.Event,
	persistedAt time.Time,
) error {
	args := make([]any, 0, len(events)*governanceAuditColumnsPerRow)
	var values strings.Builder
	for i, event := range events {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * governanceAuditColumnsPerRow
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6,
			offset+7, offset+8, offset+9, offset+10, offset+11, offset+12, offset+13,
			offset+14, offset+15,
		)
		args = append(
			args,
			governanceAuditEventID(event),
			string(event.Type),
			string(event.ActorClass),
			nullableGovernanceAuditString(event.ActorIDHash),
			nullableGovernanceAuditString(event.ServicePrincipalID),
			string(event.ScopeClass),
			nullableGovernanceAuditString(event.ScopeIDHash),
			string(event.Decision),
			event.ReasonCode,
			nullableGovernanceAuditString(event.CorrelationID),
			nullableGovernanceAuditString(event.PolicyRevisionHash),
			event.OccurredAt.UTC(),
			persistedAt,
			nullableGovernanceAuditString(event.TenantID),
			nullableGovernanceAuditString(event.WorkspaceID),
		)
	}
	query := insertGovernanceAuditEventsPrefix + values.String() + insertGovernanceAuditEventsSuffix
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("append governance audit events (%d rows): %w", len(events), err)
	}
	return nil
}

func governanceAuditEventID(event governanceaudit.Event) string {
	parts := []string{
		string(event.Type), string(event.ActorClass), event.ActorIDHash,
		event.ServicePrincipalID, string(event.ScopeClass), event.ScopeIDHash,
		string(event.Decision), event.ReasonCode, event.CorrelationID,
		event.PolicyRevisionHash, event.OccurredAt.UTC().Format(time.RFC3339Nano),
		event.TenantID, event.WorkspaceID,
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func nullableGovernanceAuditString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
