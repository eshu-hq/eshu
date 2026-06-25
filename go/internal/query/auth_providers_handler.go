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

// AuthProviderStore lists the configured interactive login providers.
// Implementations must return only auth-safe fields: no secrets, metadata URLs,
// IdP domains, org names, or group names.
type AuthProviderStore interface {
	ListLoginProviders(ctx context.Context) ([]AuthProviderItem, error)
}

// AuthProviderListHandler serves GET /api/v0/auth/providers. The route is
// PUBLIC (pre-auth) — the user is not logged in when the login page fetches it.
// The response lists configured OIDC and SAML providers so the console can
// render "Continue with …" buttons. When no providers are configured the
// handler returns an empty array rather than 404 so the UI can reliably
// distinguish "unavailable" from "none configured".
type AuthProviderListHandler struct {
	Store AuthProviderStore
}

// Mount registers the public providers discovery route.
func (h *AuthProviderListHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/providers", h.handleList)
}

func (h *AuthProviderListHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Store == nil {
		WriteJSON(w, http.StatusOK, map[string]any{"providers": []AuthProviderItem{}})
		return
	}
	items, err := h.Store.ListLoginProviders(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to list login providers")
		return
	}
	if items == nil {
		items = []AuthProviderItem{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"providers": items})
}
