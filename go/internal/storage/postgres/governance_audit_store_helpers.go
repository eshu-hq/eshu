// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

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

func buildGovernanceAuditListQuery(filter GovernanceAuditQuery) (string, []any) {
	args := []any{}
	clauses := []string{"1 = 1"}
	add := func(column string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if filter.EventType != "" {
		add("event_type", string(filter.EventType))
	}
	if filter.ActorClass != "" {
		add("actor_class", string(filter.ActorClass))
	}
	if filter.ScopeClass != "" {
		add("scope_class", string(filter.ScopeClass))
	}
	if filter.Decision != "" {
		add("decision", string(filter.Decision))
	}
	if strings.TrimSpace(filter.ReasonCode) != "" {
		add("reason_code", strings.TrimSpace(filter.ReasonCode))
	}
	if strings.TrimSpace(filter.CorrelationID) != "" {
		add("correlation_id", strings.TrimSpace(filter.CorrelationID))
	}
	if strings.TrimSpace(filter.TenantID) != "" {
		add("tenant_id", strings.TrimSpace(filter.TenantID))
	}
	if !filter.OccurredAfter.IsZero() {
		args = append(args, filter.OccurredAfter.UTC())
		clauses = append(clauses, fmt.Sprintf("occurred_at >= $%d", len(args)))
	}
	if !filter.OccurredBefore.IsZero() {
		args = append(args, filter.OccurredBefore.UTC())
		clauses = append(clauses, fmt.Sprintf("occurred_at < $%d", len(args)))
	}
	args = append(args, governanceAuditLimit(filter.Limit))
	orderDir := "ASC"
	if filter.OrderDesc {
		orderDir = "DESC"
	}
	query := fmt.Sprintf(`
SELECT event_type, actor_class, actor_id_hash, service_principal_id, scope_class,
       scope_id_hash, decision, reason_code, correlation_id, policy_revision_hash, occurred_at,
       tenant_id, workspace_id
FROM governance_audit_events
WHERE %s
ORDER BY occurred_at %s, event_id %s
LIMIT $%d
`, strings.Join(clauses, " AND "), orderDir, orderDir, len(args))
	return query, args
}

func governanceAuditLimit(limit int) int {
	if limit <= 0 {
		return defaultGovernanceAuditLimit
	}
	if limit > maxGovernanceAuditLimit {
		return maxGovernanceAuditLimit
	}
	return limit
}

func scanGovernanceAuditEvent(rows Rows) (governanceaudit.Event, error) {
	var eventType, actorClass, scopeClass, decision string
	var actorIDHash, servicePrincipalID, scopeIDHash, correlationID, policyRevisionHash sql.NullString
	var tenantID, workspaceID sql.NullString
	var event governanceaudit.Event
	if err := rows.Scan(
		&eventType,
		&actorClass,
		&actorIDHash,
		&servicePrincipalID,
		&scopeClass,
		&scopeIDHash,
		&decision,
		&event.ReasonCode,
		&correlationID,
		&policyRevisionHash,
		&event.OccurredAt,
		&tenantID,
		&workspaceID,
	); err != nil {
		if err == sql.ErrNoRows {
			return governanceaudit.Event{}, err
		}
		return governanceaudit.Event{}, fmt.Errorf("scan governance audit event: %w", err)
	}
	event.Type = governanceaudit.EventType(eventType)
	event.ActorClass = governanceaudit.ActorClass(actorClass)
	event.ActorIDHash = actorIDHash.String
	event.ServicePrincipalID = servicePrincipalID.String
	event.ScopeClass = governanceaudit.ScopeClass(scopeClass)
	event.ScopeIDHash = scopeIDHash.String
	event.Decision = governanceaudit.Decision(decision)
	event.CorrelationID = correlationID.String
	event.PolicyRevisionHash = policyRevisionHash.String
	event.TenantID = tenantID.String
	event.WorkspaceID = workspaceID.String
	normalized, err := governanceaudit.NormalizeEvent(event)
	if err != nil {
		return governanceaudit.Event{}, err
	}
	return normalized, nil
}

func applyGovernanceAuditSummaryRow(
	summary *governanceaudit.Summary,
	category string,
	name string,
	count int,
	lastOccurredAt time.Time,
) {
	if count < 0 {
		return
	}
	if lastOccurredAt.After(summary.LastOccurredAt) {
		summary.LastOccurredAt = lastOccurredAt.UTC()
	}
	switch category {
	case "total":
		summary.Total = count
	case "decision":
		summary.DecisionCounts = append(summary.DecisionCounts, governanceaudit.Count{Name: name, Count: count})
		switch governanceaudit.Decision(name) {
		case governanceaudit.DecisionAllowed:
			summary.Allowed = count
		case governanceaudit.DecisionDenied:
			summary.Denied = count
		case governanceaudit.DecisionUnavailable:
			summary.Unavailable = count
		}
	case "event_type":
		summary.EventTypeCounts = append(summary.EventTypeCounts, governanceaudit.Count{Name: name, Count: count})
	case "reason":
		summary.ReasonCounts = append(summary.ReasonCounts, governanceaudit.Count{Name: name, Count: count})
	case "actor_class":
		summary.ActorClassCounts = append(summary.ActorClassCounts, governanceaudit.Count{Name: name, Count: count})
	case "scope_class":
		summary.ScopeClassCounts = append(summary.ScopeClassCounts, governanceaudit.Count{Name: name, Count: count})
	}
}
