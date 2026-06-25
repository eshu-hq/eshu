// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

func (h *AdminIdentityReadHandler) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Audit == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin audit reader is unavailable")
		return
	}
	tenantID, ok := h.auditScope(w, r)
	if !ok {
		return
	}
	limit, err := parseAdminAuditLimit(r.URL.Query().Get("limit"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := AdminAuditQuery{
		OperatorAuthorized: true,
		EventType:          strings.TrimSpace(r.URL.Query().Get("event_type")),
		Decision:           strings.TrimSpace(r.URL.Query().Get("decision")),
		ReasonCode:         strings.TrimSpace(r.URL.Query().Get("reason_code")),
		OccurredAfter:      parseAdminAuditTime(r.URL.Query().Get("occurred_after")),
		OccurredBefore:     parseAdminAuditTime(r.URL.Query().Get("occurred_before")),
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
		WriteError(w, http.StatusInternalServerError, "failed to list audit events")
		return
	}
	out := make([]map[string]any, 0, len(events))
	for _, event := range events {
		out = append(out, adminAuditEventJSON(event))
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"events": out,
		// truncated reflects the EFFECTIVE limit applied (caller's limit, the
		// default, or the cap) — not just the hard max — so a full page is never
		// reported as complete.
		"truncated": len(events) == limit,
	})
}

func (h *AdminIdentityReadHandler) handleAuditSummary(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Audit == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin audit reader is unavailable")
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
		WriteError(w, http.StatusInternalServerError, "failed to summarize audit events")
		return
	}
	WriteJSON(w, http.StatusOK, adminAuditSummaryJSON(summary))
}

// adminAuditEventJSON projects one audit event to the audit-safe fields the
// store already exposes. actor_id_hash, scope_id_hash, and policy_revision_hash
// are intentionally omitted: they are hashed identifiers, not display values.
func adminAuditEventJSON(event governanceaudit.Event) map[string]any {
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

// adminAuditSummaryJSON projects the aggregate audit summary to safe counts.
func adminAuditSummaryJSON(summary governanceaudit.Summary) map[string]any {
	return map[string]any{
		"total":              summary.Total,
		"allowed":            summary.Allowed,
		"denied":             summary.Denied,
		"unavailable":        summary.Unavailable,
		"last_occurred_at":   summary.LastOccurredAt.UTC(),
		"event_type_counts":  adminAuditCounts(summary.EventTypeCounts),
		"decision_counts":    adminAuditCounts(summary.DecisionCounts),
		"reason_counts":      adminAuditCounts(summary.ReasonCounts),
		"actor_class_counts": adminAuditCounts(summary.ActorClassCounts),
		"scope_class_counts": adminAuditCounts(summary.ScopeClassCounts),
	}
}

func adminAuditCounts(counts []governanceaudit.Count) []map[string]any {
	out := make([]map[string]any, 0, len(counts))
	for _, count := range counts {
		out = append(out, map[string]any{"name": count.Name, "count": count.Count})
	}
	return out
}

// addOptionalTime adds a timestamp field only when it is set, so a never-set
// nullable column renders as absent rather than the zero time.
func addOptionalTime(row map[string]any, key string, value time.Time) {
	if !value.IsZero() {
		row[key] = value.UTC()
	}
}

// parseAdminAuditTime parses an RFC 3339 timestamp, returning the zero time for
// blank or malformed input so a bad filter never aborts the read.
func parseAdminAuditTime(raw string) time.Time {
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

// parseAdminAuditLimit parses and clamps a requested limit.
// A blank value returns (0, nil) and lets the store apply its default.
// A non-numeric or negative value returns an error so the handler can
// reject the request with 400 rather than silently coercing it.
// A value above maxAdminAuditEventLimit is clamped silently.
func parseAdminAuditLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		// Resolve the effective default the store applies so the handler can
		// report truncation honestly against the real page size.
		return defaultAdminAuditEventLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("limit must be a non-negative integer, got %q", raw)
	}
	if limit < 0 {
		return 0, fmt.Errorf("limit must be non-negative, got %d", limit)
	}
	if limit > maxAdminAuditEventLimit {
		return maxAdminAuditEventLimit, nil
	}
	return limit, nil
}
