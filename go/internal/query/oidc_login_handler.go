// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// OIDC login errors let provider implementations map fail-closed outcomes to
// stable HTTP statuses without leaking provider or claim details.
var (
	ErrOIDCLoginUnavailable    = errors.New("oidc login unavailable")
	ErrOIDCLoginInvalidRequest = errors.New("oidc login invalid request")
	ErrOIDCLoginDenied         = errors.New("oidc login denied")
)

// OIDCLoginService resolves generic OIDC Authorization Code login requests
// into Eshu browser-session authorization contexts.
type OIDCLoginService interface {
	StartOIDCLogin(context.Context, OIDCLoginStartRequest) (OIDCLoginStartResponse, error)
	CompleteOIDCLogin(context.Context, OIDCLoginCompleteRequest) (OIDCLoginCompleteResponse, error)
}

// OIDCLoginStartRequest selects one configured identity provider and the
// tenant/workspace boundary the dashboard session should enter after callback.
type OIDCLoginStartRequest struct {
	ProviderConfigID string
	TenantID         string
	WorkspaceID      string
	ReturnToPath     string
}

// OIDCLoginStartResponse contains the provider authorization redirect URL.
type OIDCLoginStartResponse struct {
	RedirectURL string
}

// OIDCLoginCompleteRequest carries the callback state and authorization code.
type OIDCLoginCompleteRequest struct {
	State string
	Code  string
}

// OIDCLoginCompleteResponse carries the resolved Eshu auth context and optional
// local path for post-login browser navigation.
//
// ProviderGroupHashes carries the hashed external group claims that mapped to
// this session's grants at login. They are persisted hash-only so bounded
// active-session refresh can re-resolve the group-to-role mappings and detect
// removed or tombstoned mappings rather than trusting stale role IDs alone.
type OIDCLoginCompleteResponse struct {
	Auth                AuthContext
	ProviderConfigID    string
	ProviderSubjectID   string
	ProviderGroupHashes []string
	ProviderProofAt     time.Time
	ReturnToPath        string
}

// DefaultOIDCSessionRefreshWindow bounds external IdP proof staleness for
// already-issued OIDC browser sessions.
const DefaultOIDCSessionRefreshWindow = 15 * time.Minute

// OIDCProviderLister is an optional extension on OIDCLoginService implementations
// that can enumerate the set of provider_config_ids they manage. The
// AuthProviderListHandler uses it to surface runtime-configured OIDC providers
// for the pre-auth provider discovery endpoint. Each entry is
// (ProviderConfigID, TenantID) — no client secrets, issuer URLs, or claim
// mappings are exposed. Implementations that do not list providers need not
// implement this interface.
type OIDCProviderLister interface {
	// ListOIDCProviderIDs returns the (ProviderConfigID, TenantID) pairs for
	// every OIDC provider registered at runtime. The caller is responsible for
	// deduplicating against DB rows before surfacing to clients.
	ListOIDCProviderIDs() []OIDCRegisteredProvider
}

// OIDCRegisteredProvider carries the identity fields needed for provider
// discovery. Only the safe-to-surface subset of ProviderConfig is included.
type OIDCRegisteredProvider struct {
	ProviderConfigID string
	TenantID         string
}

// OIDCLoginHandler serves generic OIDC login and callback routes.
type OIDCLoginHandler struct {
	Service              OIDCLoginService
	SessionIssuer        *BrowserSessionHandler
	SessionRefreshWindow time.Duration
	// Audit records an identity_authentication governance-audit event for
	// every callback outcome (issue #5601): allowed
	// ("sso_login_authenticated") and denied (a classification such as
	// "no_grants", carried through SSOLoginDeniedError). Nil is safe —
	// auditing is skipped, matching LocalIdentityHandler.Audit's nil-safe
	// convention.
	Audit GovernanceAuditAppender
}

// RegisteredProviders returns the set of OIDC providers managed by the Service,
// if the Service implements OIDCProviderLister. Returns nil when the Service
// does not implement the interface or when h is nil.
func (h *OIDCLoginHandler) RegisteredProviders() []OIDCRegisteredProvider {
	if h == nil || h.Service == nil {
		return nil
	}
	lister, ok := h.Service.(OIDCProviderLister)
	if !ok {
		return nil
	}
	return lister.ListOIDCProviderIDs()
}

// Mount registers OIDC login routes.
func (h *OIDCLoginHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/oidc/login", h.handleStart)
	mux.HandleFunc("GET /api/v0/auth/oidc/callback", h.handleCallback)
}

func (h *OIDCLoginHandler) handleStart(w http.ResponseWriter, r *http.Request) {
	if !h.serviceReady(w) {
		return
	}
	req := OIDCLoginStartRequest{
		ProviderConfigID: QueryParam(r, "provider_config_id"),
		TenantID:         QueryParam(r, "tenant_id"),
		WorkspaceID:      QueryParam(r, "workspace_id"),
		ReturnToPath:     safeOIDCReturnPath(QueryParam(r, "return_to")),
	}
	start, err := h.Service.StartOIDCLogin(r.Context(), req)
	if err != nil {
		writeOIDCLoginError(w, err)
		return
	}
	redirectURL := strings.TrimSpace(start.RedirectURL)
	if redirectURL == "" {
		WriteError(w, http.StatusInternalServerError, "oidc login did not return a redirect URL")
		return
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (h *OIDCLoginHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	req := OIDCLoginCompleteRequest{
		State: QueryParam(r, "state"),
		Code:  QueryParam(r, "code"),
	}
	complete, err := h.Service.CompleteOIDCLogin(r.Context(), req)
	if err != nil {
		auditOIDCSSOLogin(r, h.Audit, h.SessionIssuer.now(), err, "", "", "")
		writeOIDCLoginError(w, err)
		return
	}
	auditOIDCSSOLogin(r, h.Audit, h.SessionIssuer.now(), nil, complete.ProviderSubjectID, complete.Auth.TenantID, complete.Auth.WorkspaceID)
	proofAt := complete.ProviderProofAt.UTC()
	if proofAt.IsZero() {
		proofAt = h.SessionIssuer.now()
	}
	response, ok := h.SessionIssuer.issueBrowserSessionWithExternalAuth(w, r, complete.Auth, 0, BrowserSessionExternalAuthProof{
		ProviderConfigID: complete.ProviderConfigID,
		SubjectIDHash:    complete.ProviderSubjectID,
		GroupHashes:      append([]string(nil), complete.ProviderGroupHashes...),
		ValidatedAt:      proofAt,
		StaleAfter:       proofAt.Add(h.sessionRefreshWindow()),
	})
	if !ok {
		return
	}
	returnTo := safeOIDCReturnPath(complete.ReturnToPath)
	if returnTo == "" {
		WriteJSON(w, http.StatusCreated, response)
		return
	}
	http.Redirect(w, r, returnTo, http.StatusSeeOther)
}

func (h *OIDCLoginHandler) ready(w http.ResponseWriter) bool {
	if !h.serviceReady(w) {
		return false
	}
	if h.SessionIssuer == nil || h.SessionIssuer.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "browser session store is unavailable")
		return false
	}
	return true
}

func (h *OIDCLoginHandler) serviceReady(w http.ResponseWriter) bool {
	if h == nil || h.Service == nil {
		WriteError(w, http.StatusServiceUnavailable, "oidc login is unavailable")
		return false
	}
	return true
}

func (h *OIDCLoginHandler) sessionRefreshWindow() time.Duration {
	if h.SessionRefreshWindow > 0 {
		return h.SessionRefreshWindow
	}
	return DefaultOIDCSessionRefreshWindow
}

func writeOIDCLoginError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrOIDCLoginUnavailable):
		WriteError(w, http.StatusServiceUnavailable, "oidc login is unavailable")
	case errors.Is(err, ErrOIDCLoginInvalidRequest):
		WriteError(w, http.StatusBadRequest, "invalid oidc login request")
	case errors.Is(err, ErrOIDCLoginDenied):
		WriteError(w, http.StatusForbidden, "oidc login denied")
	default:
		WriteError(w, http.StatusInternalServerError, "oidc login failed")
	}
}

func safeOIDCReturnPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return ""
	}
	if strings.ContainsAny(path, "\r\n\t") {
		return ""
	}
	return path
}
