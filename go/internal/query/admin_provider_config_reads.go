// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"log/slog"
	"net/http"
	"strings"
)

// adminProviderConfigListLimit bounds the provider-config list response, same
// convention as adminIdentityListLimit.
const adminProviderConfigListLimit = 500

// AdminProviderConfigReadHandler serves the DB-backed identity provider-config
// CRUD read endpoints (#4966, epic #4962). No route ever returns a secret:
// only has_secret, secret_fingerprint (a non-reversible short hash of the
// envelope ciphertext), and key_id are secret-adjacent. This handler never
// imports secretcrypto — see secretcrypto_open_boundary_test.go.
type AdminProviderConfigReadHandler struct {
	Store AdminProviderConfigReadStore
}

// Mount registers the admin provider-config read routes.
func (h *AdminProviderConfigReadHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/admin/provider-configs", h.handleList)
	mux.HandleFunc("GET /api/v0/auth/admin/provider-configs/{provider_config_id}", h.handleGet)
	mux.HandleFunc("GET /api/v0/auth/admin/provider-configs/{provider_config_id}/revisions", h.handleListRevisions)
}

func (h *AdminProviderConfigReadHandler) storeReady(w http.ResponseWriter) bool {
	if h == nil || h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin provider config read store is unavailable")
		return false
	}
	return true
}

func (h *AdminProviderConfigReadHandler) adminScope(w http.ResponseWriter, r *http.Request) (tenantID string, ok bool) {
	auth, found := AuthContextFromContext(r.Context())
	auth = normalizeAuthContext(auth)
	if !found || !auth.AllScopes {
		WriteError(w, http.StatusForbidden, "all-scope admin authentication is required")
		return "", false
	}
	if auth.TenantID == "" {
		WriteError(w, http.StatusForbidden, "admin tenant scope is required")
		return "", false
	}
	return auth.TenantID, true
}

func (h *AdminProviderConfigReadHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	if !requirePermissionFeature(w, r, "identity_admin.provider_configs", permissionFeatureIdentityAdmin) {
		return
	}
	tenantID, ok := h.adminScope(w, r)
	if !ok {
		return
	}
	items, err := h.Store.ListProviderConfigDetails(r.Context(), tenantID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin list provider configs failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to list provider configs")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, providerConfigDetailJSON(item))
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"provider_configs": out,
		"truncated":        len(items) == adminProviderConfigListLimit,
	})
}

func (h *AdminProviderConfigReadHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	if !requirePermissionFeature(w, r, "identity_admin.provider_configs", permissionFeatureIdentityAdmin) {
		return
	}
	tenantID, ok := h.adminScope(w, r)
	if !ok {
		return
	}
	providerConfigID := strings.TrimSpace(PathParam(r, "provider_config_id"))
	if providerConfigID == "" {
		WriteError(w, http.StatusBadRequest, "provider_config_id is required")
		return
	}
	detail, found, err := h.Store.GetProviderConfigDetail(r.Context(), providerConfigID, tenantID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin get provider config failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to read provider config")
		return
	}
	if !found {
		WriteError(w, http.StatusNotFound, "provider config not found")
		return
	}
	WriteJSON(w, http.StatusOK, providerConfigDetailJSON(detail))
}

func (h *AdminProviderConfigReadHandler) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	if !requirePermissionFeature(w, r, "identity_admin.provider_configs", permissionFeatureIdentityAdmin) {
		return
	}
	tenantID, ok := h.adminScope(w, r)
	if !ok {
		return
	}
	providerConfigID := strings.TrimSpace(PathParam(r, "provider_config_id"))
	if providerConfigID == "" {
		WriteError(w, http.StatusBadRequest, "provider_config_id is required")
		return
	}
	items, err := h.Store.ListProviderConfigRevisions(r.Context(), providerConfigID, tenantID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin list provider config revisions failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to list provider config revisions")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row := map[string]any{
			"revision_id": item.RevisionID,
			"status":      item.Status,
			"has_secret":  item.HasSecret,
			"created_at":  item.CreatedAt,
		}
		addOptionalTime(row, "activated_at", item.ActivatedAt)
		addOptionalTime(row, "superseded_at", item.SupersededAt)
		out = append(out, row)
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"revisions": out,
	})
}

// providerConfigDetailJSON projects AdminProviderConfigDetail into the API
// response shape. It never includes a secret field — only has_secret,
// secret_fingerprint, and key_id.
func providerConfigDetailJSON(detail AdminProviderConfigDetail) map[string]any {
	row := map[string]any{
		"provider_config_id":      detail.ProviderConfigID,
		"provider_kind":           detail.ProviderKind,
		"status":                  detail.Status,
		"active_revision_id":      detail.ActiveRevisionID,
		"configuration":           detail.Configuration,
		"has_secret":              detail.HasSecret,
		"shadowed_by_environment": detail.ShadowedByEnvironment,
		"source":                  detail.Source,
		"created_at":              detail.CreatedAt,
		"updated_at":              detail.UpdatedAt,
	}
	if detail.SecretFingerprint != "" {
		row["secret_fingerprint"] = detail.SecretFingerprint
	}
	if detail.SecretKeyID != "" {
		row["key_id"] = detail.SecretKeyID
	}
	return row
}
