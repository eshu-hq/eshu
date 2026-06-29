// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"log/slog"
	"net/http"
	"strings"
)

// maxAdminAuditEventLimit bounds the audit-event read window an admin may
// request through the HTTP query string. The store applies its own cap as well;
// this keeps a hostile or buggy client from requesting an unbounded page.
const maxAdminAuditEventLimit = 500

// defaultAdminAuditEventLimit is the effective page size when the caller omits
// limit. It mirrors defaultGovernanceAuditLimit in
// internal/storage/postgres/governance_audit_store.go so the handler can report
// truncation against the limit the store actually applies.
const defaultAdminAuditEventLimit = 100

// adminIdentityListLimit is the LIMIT applied by every tenant-scoped admin
// identity read query (invitations, role assignments, roles, role grants,
// IdP providers, IdP group mappings, API tokens). It must stay in sync with
// the LIMIT 500 clause in each listAdmin*Query constant in
// internal/storage/postgres/identity_admin_reads*.go. When a response returns
// exactly this many rows the list was truncated; the handler signals this via
// a "truncated": true field so callers are not silently shown a partial view.
const adminIdentityListLimit = 500

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

// auditScope resolves the audit caller's authorization and tenant scope.
//
// Two caller classes are permitted:
//   - Shared operator (AuthModeShared): tenantID is returned empty, meaning
//     the caller sees all events across all tenants.
//   - Tenant admin (AllScopes=true + non-empty TenantID): tenantID is returned
//     as the caller's own tenant; they see only their tenant's events.
//
// Any other caller (unauthenticated, scoped without AllScopes, tenant session
// without TenantID) gets 403 and ok=false.
func (h *AdminIdentityReadHandler) auditScope(w http.ResponseWriter, r *http.Request) (tenantID string, ok bool) {
	auth, found := AuthContextFromContext(r.Context())
	if !found {
		WriteError(w, http.StatusForbidden, "authentication is required for audit access")
		return "", false
	}
	auth = normalizeAuthContext(auth)
	// Shared operator: global view, no tenant filter.
	if auth.Mode == AuthModeShared {
		return "", true
	}
	// Tenant admin: must have AllScopes and a concrete tenant.
	if auth.AllScopes && auth.TenantID != "" {
		return auth.TenantID, true
	}
	WriteError(w, http.StatusForbidden, "shared operator or all-scope tenant admin authentication is required for audit access")
	return "", false
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
	if !requirePermissionFeature(w, r, "identity_admin.invitations", permissionFeatureIdentityAdmin) {
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
	WriteJSON(w, http.StatusOK, map[string]any{
		"invitations": out,
		"truncated":   len(items) == adminIdentityListLimit,
	})
}

func (h *AdminIdentityReadHandler) handleListRoleAssignments(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	if !requirePermissionFeature(w, r, "roles_grants.assignments", permissionFeatureRolesGrants) {
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
	WriteJSON(w, http.StatusOK, map[string]any{
		"role_assignments": out,
		"truncated":        len(items) == adminIdentityListLimit,
	})
}

func (h *AdminIdentityReadHandler) handleListRoles(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	if !requirePermissionFeature(w, r, "roles_grants.roles", permissionFeatureRolesGrants) {
		return
	}
	tenantID, _, ok := h.adminScope(w, r)
	if !ok {
		return
	}
	items, grantsTruncated, err := h.Store.ListAdminRoles(r.Context(), tenantID)
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
	WriteJSON(w, http.StatusOK, map[string]any{
		"roles": out,
		// truncated when either the roles page hit its cap OR the grants read hit
		// its cap (in which case some roles show an incomplete grant set).
		"truncated": len(items) == adminIdentityListLimit || grantsTruncated,
	})
}

func (h *AdminIdentityReadHandler) handleListIdPProviders(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	if !requirePermissionFeature(w, r, "identity_admin.idp_providers", permissionFeatureIdentityAdmin) {
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
	WriteJSON(w, http.StatusOK, map[string]any{
		"providers": out,
		"truncated": len(items) == adminIdentityListLimit,
	})
}

func (h *AdminIdentityReadHandler) handleListIdPGroupMappings(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	if !requirePermissionFeature(w, r, "roles_grants.idp_group_mappings", permissionFeatureRolesGrants) {
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
	WriteJSON(w, http.StatusOK, map[string]any{
		"group_mappings": out,
		"truncated":      len(items) == adminIdentityListLimit,
	})
}

func (h *AdminIdentityReadHandler) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	if !requirePermissionFeature(w, r, "tokens.admin_list", permissionFeatureTokens) {
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
	WriteJSON(w, http.StatusOK, map[string]any{
		"tokens":    out,
		"truncated": len(items) == adminIdentityListLimit,
	})
}
