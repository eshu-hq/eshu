// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
)

// The two timeout defaults are compatibility aliases: they moved to
// queryauth (#6642) so a handler-family subpackage can name them without
// importing this package; see queryauth for the doc comments.
// browserSessionSecretBytes stays here, unexported and unmoved.
const (
	DefaultBrowserSessionIdleTimeout     = queryauth.DefaultBrowserSessionIdleTimeout
	DefaultBrowserSessionAbsoluteTimeout = queryauth.DefaultBrowserSessionAbsoluteTimeout
	browserSessionSecretBytes            = 32
)

// BrowserSessionStore is the write surface for server-managed dashboard
// sessions. It lives in queryauth (#6642) so a handler-family subpackage can
// name it without importing this package.
type BrowserSessionStore = queryauth.BrowserSessionStore

// BrowserSessionCreateRecord is the hash-only session row requested by the
// HTTP handler. It lives in queryauth (#6642).
type BrowserSessionCreateRecord = queryauth.BrowserSessionCreateRecord

// BrowserSessionExternalAuthProof carries hash-only external IdP proof metadata
// for sessions that must reauthenticate after a bounded staleness window.
type BrowserSessionExternalAuthProof struct {
	ProviderConfigID string
	SubjectIDHash    string
	GroupHashes      []string
	ValidatedAt      time.Time
	StaleAfter       time.Time
}

// BrowserSessionHandler serves dashboard session creation, revocation, and
// workspace switching routes.
type BrowserSessionHandler struct {
	Store           BrowserSessionStore
	NewSecret       func() (string, error)
	Now             func() time.Time
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
	// CookieSecure selects the Secure-attribute policy for issued session
	// and CSRF cookies. Empty defaults to CookieSecureAuto (#4964).
	CookieSecure CookieSecureMode
	// SignInPolicy resolves a per-tenant idle/absolute session timeout
	// override (issue #4968, epic #4962). Nil falls back to
	// IdleTimeout/AbsoluteTimeout (or their process-wide defaults) for every
	// tenant, matching pre-#4968 behavior. This handler is the session
	// issuer for both plain token-upgrade sessions (handleCreate) and OIDC
	// (issueBrowserSessionWithExternalAuth, called from OIDCLoginHandler).
	SignInPolicy SignInPolicyReadStore
}

// BrowserSessionResponse is returned by browser session routes. It lives in
// queryauth (#6642) so a handler-family subpackage can name it without
// importing this package.
type BrowserSessionResponse = queryauth.BrowserSessionResponse

// BrowserSessionAuthResponse is the public JSON view of a request auth
// context. It lives in queryauth (#6642).
type BrowserSessionAuthResponse = queryauth.BrowserSessionAuthResponse

// Mount registers browser session routes.
func (h *BrowserSessionHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/auth/browser-session", h.handleCreate)
	mux.HandleFunc("GET /api/v0/auth/browser-session", h.handleCurrent)
	mux.HandleFunc("DELETE /api/v0/auth/browser-session", h.handleLogout)
	mux.HandleFunc("PATCH /api/v0/auth/browser-session/context", h.handleSwitch)
}

func (h *BrowserSessionHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	auth, ok := AuthContextFromContext(r.Context())
	if !ok {
		unauthorizedResponse(w, r)
		return
	}
	auth = normalizeAuthContext(auth)
	if auth.Mode == AuthModeBrowserSession {
		WriteError(w, http.StatusBadRequest, "browser sessions must be created from an explicit API credential")
		return
	}
	if strings.TrimSpace(auth.TenantID) == "" || strings.TrimSpace(auth.WorkspaceID) == "" {
		WriteError(w, http.StatusBadRequest, "tenant_id and workspace_id are required to create a browser session")
		return
	}

	h.issueBrowserSession(w, r, auth, http.StatusCreated)
}

func (h *BrowserSessionHandler) issueBrowserSession(
	w http.ResponseWriter,
	r *http.Request,
	auth AuthContext,
	status int,
) (BrowserSessionResponse, bool) {
	return h.issueBrowserSessionWithExternalAuth(w, r, auth, status, BrowserSessionExternalAuthProof{})
}

func (h *BrowserSessionHandler) issueBrowserSessionWithExternalAuth(
	w http.ResponseWriter,
	r *http.Request,
	auth AuthContext,
	status int,
	externalAuth BrowserSessionExternalAuthProof,
) (BrowserSessionResponse, bool) {
	auth = normalizeAuthContext(auth)
	sessionSecret, err := h.newSecret()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to create browser session")
		return BrowserSessionResponse{}, false
	}
	csrfSecret, err := h.newSecret()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to create browser session")
		return BrowserSessionResponse{}, false
	}
	now := h.now()
	idleTimeout, absoluteTimeout := resolveSessionTimeouts(
		r.Context(), h.SignInPolicy, auth.TenantID, h.idleTimeout(), h.absoluteTimeout(),
	)
	idleExpiresAt := now.Add(idleTimeout)
	absoluteExpiresAt := now.Add(absoluteTimeout)
	if idleExpiresAt.After(absoluteExpiresAt) {
		idleExpiresAt = absoluteExpiresAt
	}
	record := BrowserSessionCreateRecord{
		SessionHash:                  BrowserSessionSecretHash(sessionSecret),
		CSRFTokenHash:                BrowserSessionSecretHash(csrfSecret),
		TenantID:                     auth.TenantID,
		WorkspaceID:                  auth.WorkspaceID,
		SubjectIDHash:                auth.SubjectIDHash,
		SubjectClass:                 auth.SubjectClass,
		PolicyRevisionHash:           auth.PolicyRevisionHash,
		RoleIDs:                      append([]string(nil), auth.RoleIDs...),
		AllScopes:                    auth.AllScopes,
		PermissionCatalogEnforced:    auth.PermissionCatalogEnforced,
		AllowedScopeIDs:              append([]string(nil), auth.AllowedScopeIDs...),
		AllowedRepositoryIDs:         append([]string(nil), auth.AllowedRepositoryIDs...),
		AllowedPermissionFeatures:    append([]string(nil), auth.AllowedPermissionFeatures...),
		AllowedPermissionDataClasses: append([]string(nil), auth.AllowedPermissionDataClasses...),
		ExternalProviderConfigID:     strings.TrimSpace(externalAuth.ProviderConfigID),
		ExternalSubjectIDHash:        strings.TrimSpace(externalAuth.SubjectIDHash),
		ExternalGroupHashes:          append([]string(nil), externalAuth.GroupHashes...),
		ExternalAuthValidatedAt:      externalAuth.ValidatedAt.UTC(),
		ExternalAuthStaleAfter:       externalAuth.StaleAfter.UTC(),
		IssuedAt:                     now,
		LastSeenAt:                   now,
		IdleExpiresAt:                idleExpiresAt,
		AbsoluteExpiresAt:            absoluteExpiresAt,
		UpdatedAt:                    now,
	}
	if record.SessionHash == "" || record.CSRFTokenHash == "" {
		WriteError(w, http.StatusInternalServerError, "failed to create browser session")
		return BrowserSessionResponse{}, false
	}
	if err := h.Store.CreateBrowserSession(r.Context(), record); err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to create browser session")
		return BrowserSessionResponse{}, false
	}

	sessionAuth := auth
	sessionAuth.Mode = AuthModeBrowserSession
	writeBrowserSessionCookies(
		w,
		r,
		h.cookieSecureMode(),
		sessionSecret,
		csrfSecret,
		absoluteExpiresAt,
		int(absoluteTimeout.Seconds()),
	)
	response := BrowserSessionResponse{
		Auth:              browserSessionAuthResponse(sessionAuth),
		CSRFToken:         csrfSecret,
		IdleExpiresAt:     idleExpiresAt,
		AbsoluteExpiresAt: absoluteExpiresAt,
	}
	if status > 0 {
		WriteJSON(w, status, response)
	}
	return response, true
}

func (h *BrowserSessionHandler) handleCurrent(w http.ResponseWriter, r *http.Request) {
	auth, ok := AuthContextFromContext(r.Context())
	if !ok {
		unauthorizedResponse(w, r)
		return
	}
	auth = normalizeAuthContext(auth)
	if auth.Mode != AuthModeBrowserSession {
		WriteError(w, http.StatusBadRequest, "browser session cookie authentication is required")
		return
	}
	WriteJSON(w, http.StatusOK, BrowserSessionResponse{Auth: browserSessionAuthResponse(auth)})
}

func (h *BrowserSessionHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	if !requestUsesBrowserSession(r) {
		WriteError(w, http.StatusBadRequest, "browser session cookie authentication is required")
		return
	}
	sessionHash, ok := browserSessionHashFromCookie(r)
	if !ok {
		unauthorizedResponse(w, r)
		return
	}
	if err := h.Store.RevokeBrowserSession(r.Context(), sessionHash, h.now()); err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to revoke browser session")
		return
	}
	writeBrowserSessionCookies(w, r, h.cookieSecureMode(), "", "", time.Time{}, -1)
	w.WriteHeader(http.StatusNoContent)
}

func (h *BrowserSessionHandler) handleSwitch(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	if !requestUsesBrowserSession(r) {
		WriteError(w, http.StatusBadRequest, "browser session cookie authentication is required")
		return
	}
	sessionHash, ok := browserSessionHashFromCookie(r)
	if !ok {
		unauthorizedResponse(w, r)
		return
	}
	var req struct {
		TenantID    string `json:"tenant_id"`
		WorkspaceID string `json:"workspace_id"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid browser session context request")
		return
	}
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	if req.TenantID == "" || req.WorkspaceID == "" {
		WriteError(w, http.StatusBadRequest, "tenant_id and workspace_id are required")
		return
	}
	auth, ok, err := h.Store.SwitchBrowserSessionWorkspace(
		r.Context(),
		sessionHash,
		req.TenantID,
		req.WorkspaceID,
		h.now(),
	)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to switch browser session workspace")
		return
	}
	if !ok {
		unauthorizedResponse(w, r)
		return
	}
	auth = normalizeBrowserSessionAuthContext(auth)
	WriteJSON(w, http.StatusOK, BrowserSessionResponse{Auth: browserSessionAuthResponse(auth)})
}

func (h *BrowserSessionHandler) ready(w http.ResponseWriter) bool {
	if h == nil || h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "browser session store is unavailable")
		return false
	}
	return true
}

func (h *BrowserSessionHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *BrowserSessionHandler) idleTimeout() time.Duration {
	if h.IdleTimeout > 0 {
		return h.IdleTimeout
	}
	return DefaultBrowserSessionIdleTimeout
}

func (h *BrowserSessionHandler) absoluteTimeout() time.Duration {
	if h.AbsoluteTimeout > 0 {
		return h.AbsoluteTimeout
	}
	return DefaultBrowserSessionAbsoluteTimeout
}

// cookieSecureMode normalizes h.CookieSecure, defaulting to CookieSecureAuto.
func (h *BrowserSessionHandler) cookieSecureMode() CookieSecureMode {
	return ParseCookieSecureMode(string(h.CookieSecure))
}

func (h *BrowserSessionHandler) newSecret() (string, error) {
	if h.NewSecret != nil {
		secret, err := h.NewSecret()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(secret), nil
	}
	var bytes [browserSessionSecretBytes]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func browserSessionHashFromCookie(r *http.Request) (string, bool) {
	value, ok := browserSessionCookieValue(r)
	if !ok {
		return "", false
	}
	sessionHash := BrowserSessionSecretHash(value)
	return sessionHash, sessionHash != ""
}

func requestUsesBrowserSession(r *http.Request) bool {
	auth, ok := AuthContextFromContext(r.Context())
	if !ok {
		return false
	}
	auth = normalizeAuthContext(auth)
	return auth.Mode == AuthModeBrowserSession
}

// writeBrowserSessionCookies forwards to queryauth.WriteBrowserSessionCookies.
// The implementation, and its unexported helpers (clearBrowserSessionCookies,
// browserSessionCookieSecure, browserSessionCookieNames), moved there for
// #6642 so a handler-family subpackage can issue and clear browser session
// cookies without importing this package.
func writeBrowserSessionCookies(
	w http.ResponseWriter,
	r *http.Request,
	mode CookieSecureMode,
	sessionSecret string,
	csrfSecret string,
	expiresAt time.Time,
	maxAge int,
) {
	queryauth.WriteBrowserSessionCookies(w, r, mode, sessionSecret, csrfSecret, expiresAt, maxAge)
}

// browserSessionAuthResponse forwards to queryauth.BrowserSessionAuthResponseFor.
// The implementation moved there for #6642, renamed to avoid colliding with
// the BrowserSessionAuthResponse type queryauth also exports.
func browserSessionAuthResponse(auth AuthContext) BrowserSessionAuthResponse {
	return queryauth.BrowserSessionAuthResponseFor(auth)
}
