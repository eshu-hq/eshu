// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/admin/audit"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func (h *ReadHandler) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Audit == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "admin audit reader is unavailable")
		return
	}
	if !audit.RequirePermissionFeature(w, r, "audit_export.events", queryauth.PermissionFeatureAuditExport) {
		return
	}
	tenantID, ok := h.auditScope(w, r)
	if !ok {
		return
	}
	limit, err := parseAuditLimit(r.URL.Query().Get("limit"))
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := AuditQuery{
		OperatorAuthorized: true,
		EventType:          strings.TrimSpace(r.URL.Query().Get("event_type")),
		Decision:           strings.TrimSpace(r.URL.Query().Get("decision")),
		ReasonCode:         strings.TrimSpace(r.URL.Query().Get("reason_code")),
		OccurredAfter:      parseAuditTime(r.URL.Query().Get("occurred_after")),
		OccurredBefore:     parseAuditTime(r.URL.Query().Get("occurred_before")),
		Limit:              limit,
		// Always show most-recent events first so a bounded page is useful.
		// The underlying store defaults to ASC (chronological replay order);
		// DESC is only used on the admin read path.
		OrderDesc: true,
		// TenantID is empty for shared-operator (sees all) and set for tenant
		// admins (sees only their own tenant's events).
		TenantID: tenantID,
	}
	events, err := h.Audit.ListAuditEvents(r.Context(), query)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin list audit events failed", "err", err)
		querycontract.WriteError(w, http.StatusInternalServerError, "failed to list audit events")
		return
	}
	out := make([]map[string]any, 0, len(events))
	for _, event := range events {
		out = append(out, auditEventJSON(event))
	}
	querycontract.WriteJSON(w, http.StatusOK, map[string]any{
		"events": out,
		// truncated reflects the EFFECTIVE limit applied (caller's limit, the
		// default, or the cap) — not just the hard max — so a full page is never
		// reported as complete.
		"truncated": len(events) == limit,
	})
}

func (h *ReadHandler) handleAuditSummary(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Audit == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "admin audit reader is unavailable")
		return
	}
	if !audit.RequirePermissionFeature(w, r, "audit_export.summary", queryauth.PermissionFeatureAuditExport) {
		return
	}
	tenantID, ok := h.auditScope(w, r)
	if !ok {
		return
	}
	var summary governanceaudit.Summary
	var err error
	if tenantID != "" {
		// Tenant admin: scoped summary — global/NULL-tenant events excluded.
		summary, err = h.Audit.SummarizeAuditEventsForTenant(r.Context(), tenantID)
	} else {
		// Shared operator: global summary across all tenants.
		summary, err = h.Audit.SummarizeAuditEvents(r.Context())
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "admin audit summary failed", "err", err)
		querycontract.WriteError(w, http.StatusInternalServerError, "failed to summarize audit events")
		return
	}
	querycontract.WriteJSON(w, http.StatusOK, auditSummaryJSON(summary))
}

// auditEventJSON projects one audit event to the audit-safe fields the
// store already exposes. actor_id_hash, scope_id_hash, and policy_revision_hash
// are intentionally omitted: they are hashed identifiers, not display values.
func auditEventJSON(event governanceaudit.Event) map[string]any {
	row := map[string]any{
		"event_type":  string(event.Type),
		"actor_class": string(event.ActorClass),
		"scope_class": string(event.ScopeClass),
		"decision":    string(event.Decision),
		"reason_code": event.ReasonCode,
		"occurred_at": event.OccurredAt.UTC(),
	}
	if event.ServicePrincipalID != "" {
		row["service_principal_id"] = event.ServicePrincipalID
	}
	if event.CorrelationID != "" {
		row["correlation_id"] = event.CorrelationID
	}
	return row
}

// auditSummaryJSON projects the aggregate audit summary to safe counts.
func auditSummaryJSON(summary governanceaudit.Summary) map[string]any {
	return map[string]any{
		"total":              summary.Total,
		"allowed":            summary.Allowed,
		"denied":             summary.Denied,
		"unavailable":        summary.Unavailable,
		"last_occurred_at":   summary.LastOccurredAt.UTC(),
		"event_type_counts":  auditCounts(summary.EventTypeCounts),
		"decision_counts":    auditCounts(summary.DecisionCounts),
		"reason_counts":      auditCounts(summary.ReasonCounts),
		"actor_class_counts": auditCounts(summary.ActorClassCounts),
		"scope_class_counts": auditCounts(summary.ScopeClassCounts),
	}
}

func auditCounts(counts []governanceaudit.Count) []map[string]any {
	out := make([]map[string]any, 0, len(counts))
	for _, count := range counts {
		out = append(out, map[string]any{"name": count.Name, "count": count.Count})
	}
	return out
}

// parseAuditTime parses an RFC 3339 timestamp, returning the zero time for
// blank or malformed input so a bad filter never aborts the read.
func parseAuditTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

// parseAuditLimit parses and clamps a requested limit.
// A blank value returns (0, nil) and lets the store apply its default.
// A non-numeric or negative value returns an error so the handler can
// reject the request with 400 rather than silently coercing it.
// A value above maxAuditEventLimit is clamped silently.
func parseAuditLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		// Resolve the effective default the store applies so the handler can
		// report truncation honestly against the real page size.
		return defaultAuditEventLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("limit must be a non-negative integer, got %q", raw)
	}
	if limit < 0 {
		return 0, fmt.Errorf("limit must be non-negative, got %d", limit)
	}
	if limit > maxAuditEventLimit {
		return maxAuditEventLimit, nil
	}
	return limit, nil
}
