// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

const governanceAuditEventsSchemaSQL = `
CREATE TABLE IF NOT EXISTS governance_audit_events (
    event_id TEXT PRIMARY KEY,
    event_type TEXT NOT NULL,
    actor_class TEXT NOT NULL,
    actor_id_hash TEXT NULL,
    service_principal_id TEXT NULL,
    scope_class TEXT NOT NULL,
    scope_id_hash TEXT NULL,
    decision TEXT NOT NULL,
    reason_code TEXT NOT NULL,
    correlation_id TEXT NULL,
    policy_revision_hash TEXT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    persisted_at TIMESTAMPTZ NOT NULL,
    tenant_id TEXT NULL,
    workspace_id TEXT NULL
);

ALTER TABLE governance_audit_events ADD COLUMN IF NOT EXISTS tenant_id TEXT NULL;
ALTER TABLE governance_audit_events ADD COLUMN IF NOT EXISTS workspace_id TEXT NULL;

CREATE INDEX IF NOT EXISTS governance_audit_events_query_idx
    ON governance_audit_events (
        event_type,
        actor_class,
        scope_class,
        decision,
        occurred_at ASC,
        event_id ASC
    );

CREATE INDEX IF NOT EXISTS governance_audit_events_correlation_idx
    ON governance_audit_events (correlation_id, occurred_at ASC, event_id ASC)
    WHERE correlation_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS governance_audit_events_reason_idx
    ON governance_audit_events (reason_code, occurred_at ASC, event_id ASC);

CREATE INDEX IF NOT EXISTS governance_audit_events_tenant_idx
    ON governance_audit_events (tenant_id, occurred_at ASC, event_id ASC)
    WHERE tenant_id IS NOT NULL;
`

const insertGovernanceAuditEventsPrefix = `
INSERT INTO governance_audit_events (
    event_id,
    event_type,
    actor_class,
    actor_id_hash,
    service_principal_id,
    scope_class,
    scope_id_hash,
    decision,
    reason_code,
    correlation_id,
    policy_revision_hash,
    occurred_at,
    persisted_at,
    tenant_id,
    workspace_id
) VALUES `

const insertGovernanceAuditEventsSuffix = `
ON CONFLICT (event_id) DO NOTHING
`

const governanceAuditSummarySQL = `
WITH base AS (
    SELECT event_type, actor_class, scope_class, decision, reason_code, occurred_at
    FROM governance_audit_events
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

// GovernanceAuditEventsSchemaSQL returns the private audit sink DDL.
func GovernanceAuditEventsSchemaSQL() string {
	return governanceAuditEventsSchemaSQL
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

func governanceAuditEventsBootstrapDefinition() Definition {
	return Definition{
		Name: "governance_audit_events",
		Path: "schema/data-plane/postgres/006b_governance_audit_events.sql",
		SQL:  GovernanceAuditEventsSchemaSQL(),
	}
}

func init() {
	bootstrapDefinitions = append(bootstrapDefinitions, governanceAuditEventsBootstrapDefinition())
}
