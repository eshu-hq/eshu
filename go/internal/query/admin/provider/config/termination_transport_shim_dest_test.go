// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config_test

// D0 termination-transport shim: destination-owned terminated handler copy (stdlib only). See termination_transport_shim_test.go for the theory.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// shimDestReadHandler is the destination-owned shape: a terminated
// handler that holds no store, no *sql.DB, and no queue handle — only the
// port base URL and an HTTP client. It owns the route surface (caller
// gate, status mapping, projection, envelope) and calls the versioned
// port for data.
type shimDestReadHandler struct {
	PortBaseURL string
	Client      *http.Client
}

func (h *shimDestReadHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/admin/provider-configs", h.handleList)
	mux.HandleFunc("GET /api/v0/auth/admin/provider-configs/{provider_config_id}", h.handleGet)
	mux.HandleFunc("GET /api/v0/auth/admin/provider-configs/{provider_config_id}/revisions", h.handleListRevisions)
}

// shimDestBounds is the caller-authorization bound the destination
// resolved itself (production: dest auth middleware). The test stand-in
// reads the same context the reference path reads; in production this
// resolution is dest-owned and only the asserted headers cross to core.
type shimDestBounds struct {
	authCtx auth.AuthContext
	found   bool
}

func shimDestResolveBounds(r *http.Request) shimDestBounds {
	authCtx, found := auth.AuthContextFromContext(r.Context())
	return shimDestBounds{authCtx: auth.NormalizeAuthContext(authCtx), found: found}
}

// gate mirrors the reference handler's gate order exactly: store
// readiness, then the permission feature, then all-scopes + tenant. Gate
// order is caller-visible (deny precedence), so the harness pins it.
func (h *shimDestReadHandler) gate(w http.ResponseWriter, r *http.Request, bounds shimDestBounds) (string, bool) {
	if h == nil || h.Client == nil || strings.TrimSpace(h.PortBaseURL) == "" {
		shimWriteError(w, http.StatusServiceUnavailable, shimStoreUnavailableMessage)
		return "", false
	}
	tenantID, ok := shimDestTenant(w, bounds)
	if !ok {
		return "", false
	}
	return tenantID, true
}

func shimDestTenant(w http.ResponseWriter, bounds shimDestBounds) (string, bool) {
	if !boundsAllowedFeature(bounds) {
		shimWritePermissionDenied(w)
		return "", false
	}
	if !bounds.found || !bounds.authCtx.AllScopes {
		shimWriteError(w, http.StatusForbidden, shimScopeRequiredMessage)
		return "", false
	}
	if bounds.authCtx.TenantID == "" {
		shimWriteError(w, http.StatusForbidden, shimTenantRequiredMessage)
		return "", false
	}
	return bounds.authCtx.TenantID, true
}

// boundsAllowedFeature mirrors auth.AllowsPermissionFeature for the
// identity-admin feature: unenforced callers (no context, unenforced
// catalog, shared mode) pass; enforced scoped callers need the feature.
func boundsAllowedFeature(bounds shimDestBounds) bool {
	if !bounds.found || !bounds.authCtx.PermissionCatalogEnforced || bounds.authCtx.Mode == auth.AuthModeShared {
		return true
	}
	for _, f := range bounds.authCtx.AllowedPermissionFeatures {
		if f == shimPermissionFeature || f == "*" {
			return true
		}
	}
	return false
}

// portRequest calls one port endpoint with the asserted caller bounds.
// The port server re-validates every bound before touching the store.
func (h *shimDestReadHandler) portRequest(ctx context.Context, path, tenantID string, bounds shimDestBounds, query map[string]string) (int, []byte) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.PortBaseURL+path, nil)
	if err != nil {
		return http.StatusInternalServerError, nil
	}
	q := req.URL.Query()
	q.Set("tenant_id", tenantID)
	for k, v := range query {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set(shimHeaderAllScopes, "true")
	req.Header.Set(shimHeaderTenant, tenantID)
	if bounds.authCtx.PermissionCatalogEnforced {
		req.Header.Set(shimHeaderEnforced, "true")
		req.Header.Set(shimHeaderFeatures, strings.Join(bounds.authCtx.AllowedPermissionFeatures, ","))
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return http.StatusInternalServerError, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func (h *shimDestReadHandler) handleList(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.gate(w, r, shimDestResolveBounds(r))
	if !ok {
		return
	}
	status, body := h.portRequest(r.Context(), "/v0/port/provider-configs", tenantID, shimDestResolveBounds(r), nil)
	if status != http.StatusOK {
		shimWriteError(w, status, shimListFailedMessage)
		return
	}
	var payload struct {
		Details []shimDetailDTO `json:"details"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		shimWriteError(w, http.StatusInternalServerError, shimListFailedMessage)
		return
	}
	out := make([]map[string]any, 0, len(payload.Details))
	for _, item := range payload.Details {
		out = append(out, shimDestProjectDetail(item))
	}
	shimWriteJSON(w, http.StatusOK, map[string]any{
		"provider_configs": out,
		"truncated":        len(payload.Details) == shimProviderConfigListLimit,
	})
}

func (h *shimDestReadHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.gate(w, r, shimDestResolveBounds(r))
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("provider_config_id"))
	if id == "" {
		shimWriteError(w, http.StatusBadRequest, shimIDRequiredMessage)
		return
	}
	status, body := h.portRequest(r.Context(), "/v0/port/provider-configs/detail", tenantID, shimDestResolveBounds(r),
		map[string]string{"provider_config_id": id})
	switch status {
	case http.StatusOK:
		var detail shimDetailDTO
		if err := json.Unmarshal(body, &detail); err != nil {
			shimWriteError(w, http.StatusInternalServerError, shimReadFailedMessage)
			return
		}
		shimWriteJSON(w, http.StatusOK, shimDestProjectDetail(detail))
	case http.StatusNotFound:
		shimWriteError(w, http.StatusNotFound, shimNotFoundMessage)
	default:
		shimWriteError(w, http.StatusInternalServerError, shimReadFailedMessage)
	}
}

func (h *shimDestReadHandler) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.gate(w, r, shimDestResolveBounds(r))
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("provider_config_id"))
	if id == "" {
		shimWriteError(w, http.StatusBadRequest, shimIDRequiredMessage)
		return
	}
	status, body := h.portRequest(r.Context(), "/v0/port/provider-configs/revisions", tenantID, shimDestResolveBounds(r),
		map[string]string{"provider_config_id": id})
	if status != http.StatusOK {
		shimWriteError(w, http.StatusInternalServerError, shimRevisionsFailedMessage)
		return
	}
	var payload struct {
		Revisions []shimRevisionDTO `json:"revisions"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		shimWriteError(w, http.StatusInternalServerError, shimRevisionsFailedMessage)
		return
	}
	out := make([]map[string]any, 0, len(payload.Revisions))
	for _, item := range payload.Revisions {
		row := map[string]any{
			"revision_id": item.RevisionID,
			"status":      item.Status,
			"has_secret":  item.HasSecret,
			"created_at":  item.CreatedAt,
		}
		if item.ActivatedAt != nil {
			row["activated_at"] = *item.ActivatedAt
		}
		if item.SupersededAt != nil {
			row["superseded_at"] = *item.SupersededAt
		}
		out = append(out, row)
	}
	shimWriteJSON(w, http.StatusOK, map[string]any{"revisions": out})
}

// shimDestProjectDetail is the dest-owned copy of the reference
// providerConfigDetailJSON projection: same keys, same conditional
// secret-adjacent fields, no secret ever.
func shimDestProjectDetail(d shimDetailDTO) map[string]any {
	row := map[string]any{
		"provider_config_id":      d.ProviderConfigID,
		"provider_kind":           d.ProviderKind,
		"status":                  d.Status,
		"active_revision_id":      d.ActiveRevisionID,
		"configuration":           d.Configuration,
		"has_secret":              d.HasSecret,
		"shadowed_by_environment": d.ShadowedByEnvironment,
		"managed_by":              d.ManagedBy,
		"created_at":              d.CreatedAt,
		"updated_at":              d.UpdatedAt,
	}
	if d.SecretFingerprint != "" {
		row["secret_fingerprint"] = d.SecretFingerprint
	}
	if d.SecretKeyID != "" {
		row["key_id"] = d.SecretKeyID
	}
	return row
}

// shimWriteJSON / shimWriteError mirror querycontract's writers
// byte-for-byte (Content-Type, status, non-escaped encoding): the
// envelope writer is versioned contract code the destination vendors.
func shimWriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", shimJSONContentType)
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func shimWriteError(w http.ResponseWriter, status int, message string) {
	shimWriteJSON(w, status, map[string]any{"error": http.StatusText(status), "detail": message})
}

func shimWritePermissionDenied(w http.ResponseWriter) {
	shimWriteJSON(w, http.StatusForbidden, shimPermissionDeniedBody{
		Error: &shimErrorEnvelope{
			Code:       shimPermissionDeniedCode,
			Message:    shimPermissionDeniedMessage,
			Capability: shimPermissionCapability,
		},
	})
}
