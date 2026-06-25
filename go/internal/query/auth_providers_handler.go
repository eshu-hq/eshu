// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
)

// AuthProviderItem is a single configured login provider safe to expose
// pre-authentication. It carries only the opaque provider_config_id required by
// the begin*Login helpers and a safe generic display label derived from the
// provider_kind. No domain, metadata URL, IdP entity ID, client ID, org name,
// group name, or credential is included.
type AuthProviderItem struct {
	// ProviderConfigID is the opaque operator-assigned config identifier.
	// It is intentionally exposed because the OIDC/SAML redirect endpoints
	// require it as a path or query parameter; operators choose login-facing IDs.
	ProviderConfigID string `json:"provider_config_id"`
	// DisplayLabel is a safe human-readable label for the login button.
	// Derived generically from ProviderKind ("Single sign-on (OIDC)" or
	// "Single sign-on (SAML)") and never echoes a domain, org, or metadata name.
	DisplayLabel string `json:"display_label"`
	// ProviderKind is the protocol class: "oidc" or "saml". Used by the UI to
	// select the correct begin*Login redirect helper.
	ProviderKind string `json:"provider_kind"`
}

// AuthProviderStore lists the configured interactive login providers scoped to
// one tenant. Implementations must return only auth-safe fields: no secrets,
// metadata URLs, IdP domains, org names, or group names. tenantID must be
// non-empty; callers that cannot resolve a tenant must return an empty list
// rather than invoking ListLoginProviders with an empty tenantID.
type AuthProviderStore interface {
	ListLoginProviders(ctx context.Context, tenantID string) ([]AuthProviderItem, error)
}

// AuthProviderListHandler serves GET /api/v0/auth/providers. The route is
// PUBLIC (pre-auth) — the user is not logged in when the login page fetches it.
// The response lists OIDC and SAML providers configured for the tenant
// identified by the required tenant_id query parameter. When tenant_id is
// absent or empty the handler returns an empty array — it never falls back to a
// global cross-tenant scan. When no providers are configured the handler also
// returns an empty array so the UI can reliably distinguish "unavailable" from
// "none configured". The response carries Cache-Control: public, max-age=60 to
// reduce anonymous DB load; CDN or proxy caches may serve stale data for up to
// 60 seconds.
type AuthProviderListHandler struct {
	Store AuthProviderStore
}

// Mount registers the public providers discovery route.
func (h *AuthProviderListHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/providers", h.handleList)
}

func (h *AuthProviderListHandler) handleList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=60")
	if h == nil || h.Store == nil {
		WriteJSON(w, http.StatusOK, map[string]any{"providers": []AuthProviderItem{}})
		return
	}
	tenantID := QueryParam(r, "tenant_id")
	if tenantID == "" {
		// No tenant can be resolved pre-auth without an explicit tenant_id.
		// Return an empty list rather than performing a global cross-tenant scan.
		WriteJSON(w, http.StatusOK, map[string]any{"providers": []AuthProviderItem{}})
		return
	}
	items, err := h.Store.ListLoginProviders(r.Context(), tenantID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to list login providers")
		return
	}
	if items == nil {
		items = []AuthProviderItem{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"providers": items})
}
