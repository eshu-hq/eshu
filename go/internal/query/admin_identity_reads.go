package query

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// maxAdminAuditEventLimit bounds the audit-event read window an admin may
// request through the HTTP query string. The store applies its own cap as well;
// this keeps a hostile or buggy client from requesting an unbounded page.
const maxAdminAuditEventLimit = 500

// AdminIdentityReadHandler serves tenant-scoped admin read endpoints for the
// console admin UX: invitations, role assignments, roles+grants, IdP providers,
// IdP group->role mappings, the tenant-wide API token list, and audit links.
//
// Every route requires all-scope admin authentication and reads strictly within
// the caller's own tenant/workspace. No handler returns a secret, hash, invite
// code, credential handle, or external group hash. These are read-only; all
// mutations land in later PRs.
type AdminIdentityReadHandler struct {
	Store AdminIdentityReadStore
	Audit AdminGovernanceAuditReader
}

// Mount registers the admin identity read routes.
func (h *AdminIdentityReadHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/local/invitations", h.handleListInvitations)
	mux.HandleFunc("GET /api/v0/auth/admin/role-assignments", h.handleListRoleAssignments)
	mux.HandleFunc("GET /api/v0/auth/admin/roles", h.handleListRoles)
	mux.HandleFunc("GET /api/v0/auth/admin/idp-providers", h.handleListIdPProviders)
	mux.HandleFunc("GET /api/v0/auth/admin/idp-group-mappings", h.handleListIdPGroupMappings)
	mux.HandleFunc("GET /api/v0/auth/admin/api-tokens", h.handleListAPITokens)
	mux.HandleFunc("GET /api/v0/auth/admin/audit/events", h.handleListAuditEvents)
	mux.HandleFunc("GET /api/v0/auth/admin/audit/summary", h.handleAuditSummary)
}

// adminScope resolves the all-scope admin caller's tenant/workspace. It writes
// the appropriate error and returns ok=false when the caller is not an
// all-scope admin or carries no tenant. Tenant-scoped admin reads must never run
// without a concrete tenant, so a blank tenant is rejected rather than allowed
// to list across tenants.
func (h *AdminIdentityReadHandler) adminScope(w http.ResponseWriter, r *http.Request) (tenantID, workspaceID string, ok bool) {
	auth, found := AuthContextFromContext(r.Context())
	auth = normalizeAuthContext(auth)
	if !found || !auth.AllScopes {
		WriteError(w, http.StatusForbidden, "all-scope admin authentication is required")
		return "", "", false
	}
	if auth.TenantID == "" {
		WriteError(w, http.StatusForbidden, "admin tenant scope is required")
		return "", "", false
	}
	return auth.TenantID, auth.WorkspaceID, true
}

// storeReady reports whether the read store is wired, writing 503 when not.
func (h *AdminIdentityReadHandler) storeReady(w http.ResponseWriter) bool {
	if h == nil || h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin identity read store is unavailable")
		return false
	}
	return true
}

func (h *AdminIdentityReadHandler) handleListInvitations(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	tenantID, workspaceID, ok := h.adminScope(w, r)
	if !ok {
		return
	}
	items, err := h.Store.ListAdminInvitations(r.Context(), tenantID, workspaceID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin list invitations failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to list invitations")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row := map[string]any{
			"invite_id":    item.InviteID,
			"role_id":      item.RoleID,
			"status":       item.Status,
			"expires_at":   item.ExpiresAt,
			"created_at":   item.CreatedAt,
			"updated_at":   item.UpdatedAt,
			"tenant_id":    item.TenantID,
			"workspace_id": item.WorkspaceID,
		}
		addOptionalTime(row, "accepted_at", item.AcceptedAt)
		addOptionalTime(row, "revoked_at", item.RevokedAt)
		out = append(out, row)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"invitations": out})
}

func (h *AdminIdentityReadHandler) handleListRoleAssignments(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	tenantID, workspaceID, ok := h.adminScope(w, r)
	if !ok {
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	items, err := h.Store.ListAdminRoleAssignments(r.Context(), tenantID, workspaceID, userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin list role assignments failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to list role assignments")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row := map[string]any{
			"user_id":           item.UserID,
			"role_id":           item.RoleID,
			"assignment_source": item.AssignmentSource,
			"status":            item.Status,
			"effective_at":      item.EffectiveAt,
			"tenant_id":         item.TenantID,
			"workspace_id":      item.WorkspaceID,
		}
		addOptionalTime(row, "expires_at", item.ExpiresAt)
		out = append(out, row)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"role_assignments": out})
}

func (h *AdminIdentityReadHandler) handleListRoles(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	tenantID, _, ok := h.adminScope(w, r)
	if !ok {
		return
	}
	items, err := h.Store.ListAdminRoles(r.Context(), tenantID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin list roles failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to list roles")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		grants := make([]map[string]any, 0, len(item.Grants))
		for _, grant := range item.Grants {
			grants = append(grants, map[string]any{
				"grant_id":    grant.GrantID,
				"action":      grant.Action,
				"feature":     grant.Feature,
				"data_class":  grant.DataClass,
				"scope_class": grant.ScopeClass,
				"status":      grant.Status,
			})
		}
		out = append(out, map[string]any{
			"role_id":  item.RoleID,
			"status":   item.Status,
			"built_in": item.BuiltIn,
			"grants":   grants,
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"roles": out})
}

func (h *AdminIdentityReadHandler) handleListIdPProviders(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	tenantID, _, ok := h.adminScope(w, r)
	if !ok {
		return
	}
	items, err := h.Store.ListAdminIdPProviders(r.Context(), tenantID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin list idp providers failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to list idp providers")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"provider_config_id": item.ProviderConfigID,
			"provider_kind":      item.ProviderKind,
			"status":             item.Status,
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"providers": out})
}

func (h *AdminIdentityReadHandler) handleListIdPGroupMappings(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	tenantID, workspaceID, ok := h.adminScope(w, r)
	if !ok {
		return
	}
	items, err := h.Store.ListAdminIdPGroupMappings(r.Context(), tenantID, workspaceID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin list idp group mappings failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to list idp group mappings")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row := map[string]any{
			"mapping_ref":        item.MappingRef,
			"provider_config_id": item.ProviderConfigID,
			"role_id":            item.RoleID,
			"status":             item.Status,
			"effective_at":       item.EffectiveAt,
			"tenant_id":          item.TenantID,
			"workspace_id":       item.WorkspaceID,
		}
		addOptionalTime(row, "expires_at", item.ExpiresAt)
		out = append(out, row)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"group_mappings": out})
}

func (h *AdminIdentityReadHandler) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	tenantID, workspaceID, ok := h.adminScope(w, r)
	if !ok {
		return
	}
	items, err := h.Store.ListAdminAPITokens(r.Context(), tenantID, workspaceID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin list api tokens failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to list api tokens")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row := map[string]any{
			"token_id":     item.TokenID,
			"token_class":  item.TokenClass,
			"status":       item.Status,
			"issued_at":    item.IssuedAt,
			"tenant_id":    item.TenantID,
			"workspace_id": item.WorkspaceID,
		}
		if item.UserID != "" {
			row["user_id"] = item.UserID
		}
		if item.ServicePrincipalID != "" {
			row["service_principal_id"] = item.ServicePrincipalID
		}
		addOptionalTime(row, "expires_at", item.ExpiresAt)
		addOptionalTime(row, "revoked_at", item.RevokedAt)
		out = append(out, row)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

func (h *AdminIdentityReadHandler) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Audit == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin audit reader is unavailable")
		return
	}
	if _, _, ok := h.adminScope(w, r); !ok {
		return
	}
	query := AdminAuditQuery{
		OperatorAuthorized: true,
		EventType:          strings.TrimSpace(r.URL.Query().Get("event_type")),
		Decision:           strings.TrimSpace(r.URL.Query().Get("decision")),
		ReasonCode:         strings.TrimSpace(r.URL.Query().Get("reason_code")),
		OccurredAfter:      parseAdminAuditTime(r.URL.Query().Get("occurred_after")),
		OccurredBefore:     parseAdminAuditTime(r.URL.Query().Get("occurred_before")),
		Limit:              parseAdminAuditLimit(r.URL.Query().Get("limit")),
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
	WriteJSON(w, http.StatusOK, map[string]any{"events": out})
}

func (h *AdminIdentityReadHandler) handleAuditSummary(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Audit == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin audit reader is unavailable")
		return
	}
	if _, _, ok := h.adminScope(w, r); !ok {
		return
	}
	summary, err := h.Audit.SummarizeAuditEvents(r.Context())
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

// parseAdminAuditLimit clamps a requested limit to [0, maxAdminAuditEventLimit].
// A non-positive or unparseable value yields 0, letting the store apply its
// default.
func parseAdminAuditLimit(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 0 {
		return 0
	}
	if limit > maxAdminAuditEventLimit {
		return maxAdminAuditEventLimit
	}
	return limit
}
