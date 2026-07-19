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
	// IconHint is a generic icon selector for the login button, derived from
	// ProviderKind the same way DisplayLabel is (see displayLabelForKind /
	// iconHintForKind in cmd/api/auth_providers.go). It is intentionally
	// coarse ("oidc" / "saml") rather than a brand identifier (e.g. "okta"):
	// the backend never learns or echoes which specific IdP brand a DB- or
	// env-registered provider is, only its protocol class. Kept as a field
	// distinct from ProviderKind so the console can change icon selection
	// independently of the value used to pick the redirect helper.
	IconHint string `json:"icon_hint"`
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
// The response is the tenant's derived AuthPosture (issue #5165, F-4): the
// configured OIDC/SAML providers, whether the local password form is
// offered, and whether self-service personal API tokens are offered — one
// discovery call instead of the console separately fetching providers and
// the sign-in policy. When tenant_id is absent or empty the handler returns
// the safe zero-configuration default — it never falls back to a global
// cross-tenant scan. When no providers are configured the handler also
// returns an empty providers array so the UI can reliably distinguish
// "unavailable" from "none configured". The response carries
// Cache-Control: public, max-age=60 to reduce anonymous DB load; CDN or
// proxy caches may serve stale data for up to 60 seconds — an admin
// enabling/disabling a provider is visible on the login page's NEXT
// (uncached) load, matching issue #5165's no-restart-required requirement.
type AuthProviderListHandler struct {
	Store AuthProviderStore
	// Policy supplies the tenant's sign-in policy for the
	// local_login_offered field. Nil is safe (matches Store's nil-safe
	// convention): local_login_offered then defaults to true, the same
	// default DeriveAuthPosture applies for an unwired policy store.
	Policy SignInPolicyReadStore
}

// Mount registers the public providers discovery route.
func (h *AuthProviderListHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/providers", h.handleList)
}

func (h *AuthProviderListHandler) handleList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=60")
	if h == nil || h.Store == nil {
		WriteJSON(w, http.StatusOK, authPostureJSON(AuthPosture{
			Providers:                []AuthProviderItem{},
			LocalLoginOffered:        true,
			SelfServiceTokensOffered: true,
		}))
		return
	}
	tenantID := QueryParam(r, "tenant_id")
	posture, err := DeriveAuthPosture(r.Context(), h.Store, h.Policy, tenantID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to list login providers")
		return
	}
	WriteJSON(w, http.StatusOK, authPostureJSON(posture))
}

// authPostureJSON projects AuthPosture into the exact response map shape
// (rather than relying on struct JSON tags directly) so the wire shape stays
// an explicit, reviewable contract independent of Go field ordering.
func authPostureJSON(posture AuthPosture) map[string]any {
	return map[string]any{
		"providers":                   posture.Providers,
		"local_login_offered":         posture.LocalLoginOffered,
		"self_service_tokens_offered": posture.SelfServiceTokensOffered,
	}
}
