// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/admin/audit"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// providerConfigListLimit bounds the provider-config list response, same
// convention as adminIdentityListLimit.
const providerConfigListLimit = 500

// ReadHandler serves the DB-backed identity provider-config
// CRUD read endpoints (#4966, epic #4962). No route ever returns a secret:
// only has_secret, secret_fingerprint (a non-reversible short hash of the
// envelope ciphertext), and key_id are secret-adjacent. This handler never
// imports secretcrypto — see secretcrypto_open_boundary_test.go.
type ReadHandler struct {
	Store ReadStore
}

// Mount registers the admin provider-config read routes.
func (h *ReadHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/admin/provider-configs", h.handleList)
	mux.HandleFunc("GET /api/v0/auth/admin/provider-configs/{provider_config_id}", h.handleGet)
	mux.HandleFunc("GET /api/v0/auth/admin/provider-configs/{provider_config_id}/revisions", h.handleListRevisions)
}

func (h *ReadHandler) storeReady(w http.ResponseWriter) bool {
	if h == nil || h.Store == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "admin provider config read store is unavailable")
		return false
	}
	return true
}

func (h *ReadHandler) adminScope(w http.ResponseWriter, r *http.Request) (tenantID string, ok bool) {
	auth, found := queryauth.AuthContextFromContext(r.Context())
	auth = queryauth.NormalizeAuthContext(auth)
	if !found || !auth.AllScopes {
		querycontract.WriteError(w, http.StatusForbidden, "all-scope admin authentication is required")
		return "", false
	}
	if auth.TenantID == "" {
		querycontract.WriteError(w, http.StatusForbidden, "admin tenant scope is required")
		return "", false
	}
	return auth.TenantID, true
}

func (h *ReadHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	if !audit.RequirePermissionFeature(w, r, "identity_admin.provider_configs", queryauth.PermissionFeatureIdentityAdmin) {
		return
	}
	tenantID, ok := h.adminScope(w, r)
	if !ok {
		return
	}
	items, err := h.Store.ListProviderConfigDetails(r.Context(), tenantID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin list provider configs failed", "err", err)
		querycontract.WriteError(w, http.StatusInternalServerError, "failed to list provider configs")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, providerConfigDetailJSON(item))
	}
	querycontract.WriteJSON(w, http.StatusOK, map[string]any{
		"provider_configs": out,
		"truncated":        len(items) == providerConfigListLimit,
	})
}

func (h *ReadHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	if !audit.RequirePermissionFeature(w, r, "identity_admin.provider_configs", queryauth.PermissionFeatureIdentityAdmin) {
		return
	}
	tenantID, ok := h.adminScope(w, r)
	if !ok {
		return
	}
	providerConfigID := strings.TrimSpace(querycontract.PathParam(r, "provider_config_id"))
	if providerConfigID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "provider_config_id is required")
		return
	}
	detail, found, err := h.Store.GetProviderConfigDetail(r.Context(), providerConfigID, tenantID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin get provider config failed", "err", err)
		querycontract.WriteError(w, http.StatusInternalServerError, "failed to read provider config")
		return
	}
	if !found {
		querycontract.WriteError(w, http.StatusNotFound, "provider config not found")
		return
	}
	querycontract.WriteJSON(w, http.StatusOK, providerConfigDetailJSON(detail))
}

func (h *ReadHandler) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	if !audit.RequirePermissionFeature(w, r, "identity_admin.provider_configs", queryauth.PermissionFeatureIdentityAdmin) {
		return
	}
	tenantID, ok := h.adminScope(w, r)
	if !ok {
		return
	}
	providerConfigID := strings.TrimSpace(querycontract.PathParam(r, "provider_config_id"))
	if providerConfigID == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "provider_config_id is required")
		return
	}
	items, err := h.Store.ListProviderConfigRevisions(r.Context(), providerConfigID, tenantID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin list provider config revisions failed", "err", err)
		querycontract.WriteError(w, http.StatusInternalServerError, "failed to list provider config revisions")
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
		audit.AddOptionalTime(row, "activated_at", item.ActivatedAt)
		audit.AddOptionalTime(row, "superseded_at", item.SupersededAt)
		out = append(out, row)
	}
	querycontract.WriteJSON(w, http.StatusOK, map[string]any{
		"revisions": out,
	})
}

// providerConfigDetailJSON projects Detail into the API
// response shape. It never includes a secret field — only has_secret,
// secret_fingerprint, and key_id.
func providerConfigDetailJSON(detail Detail) map[string]any {
	row := map[string]any{
		"provider_config_id":      detail.ProviderConfigID,
		"provider_kind":           detail.ProviderKind,
		"status":                  detail.Status,
		"active_revision_id":      detail.ActiveRevisionID,
		"configuration":           detail.Configuration,
		"has_secret":              detail.HasSecret,
		"shadowed_by_environment": detail.ShadowedByEnvironment,
		"managed_by":              detail.ManagedBy,
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
