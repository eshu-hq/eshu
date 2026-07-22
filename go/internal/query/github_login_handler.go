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

// GitHub sign-in errors (issue #5166, F-5) let the connector map fail-closed
// outcomes to stable HTTP statuses without leaking provider, org, or team
// details. github.com user login is plain OAuth2, not OIDC — see
// internal/githublogin's doc.go — so these are distinct from
// ErrOIDCLogin*, but GitHubLoginHandler maps them to the identical
// 503/400/403 shape as OIDCLoginHandler.
var (
	ErrGitHubLoginUnavailable    = errors.New("github login unavailable")
	ErrGitHubLoginInvalidRequest = errors.New("github login invalid request")
	ErrGitHubLoginDenied         = errors.New("github login denied")
)

// GitHubLoginService resolves GitHub Authorization Code login requests into
// Eshu browser-session authorization contexts.
type GitHubLoginService interface {
	StartGitHubLogin(context.Context, GitHubLoginStartRequest) (GitHubLoginStartResponse, error)
	CompleteGitHubLogin(context.Context, GitHubLoginCompleteRequest) (GitHubLoginCompleteResponse, error)
}

// GitHubLoginStartRequest selects one configured GitHub provider and the
// tenant/workspace boundary the dashboard session should enter after
// callback.
type GitHubLoginStartRequest struct {
	ProviderConfigID string
	TenantID         string
	WorkspaceID      string
	ReturnToPath     string
}

// GitHubLoginStartResponse contains the provider authorization redirect URL.
type GitHubLoginStartResponse struct {
	RedirectURL string
}

// GitHubLoginCompleteRequest carries the callback state and authorization
// code.
type GitHubLoginCompleteRequest struct {
	State string
	Code  string
}

// GitHubLoginCompleteResponse carries the resolved Eshu auth context and
// optional local path for post-login browser navigation, mirroring
// OIDCLoginCompleteResponse.
type GitHubLoginCompleteResponse struct {
	Auth                AuthContext
	ProviderConfigID    string
	ProviderSubjectID   string
	ProviderGroupHashes []string
	ProviderProofAt     time.Time
	ReturnToPath        string
}

// DefaultGitHubSessionRefreshWindow bounds external-proof staleness for
// already-issued GitHub browser sessions, mirroring
// DefaultOIDCSessionRefreshWindow.
const DefaultGitHubSessionRefreshWindow = 15 * time.Minute

// GitHubProviderLister is an optional extension on GitHubLoginService
// implementations that can enumerate the set of provider_config_ids they
// manage, mirroring OIDCProviderLister.
type GitHubProviderLister interface {
	ListGitHubProviderIDs() []GitHubRegisteredProvider
}

// GitHubRegisteredProvider carries the identity fields needed for provider
// discovery. Only the safe-to-surface subset of ProviderConfig is included —
// no client id, secret, base URL, or allowed-org list.
type GitHubRegisteredProvider struct {
	ProviderConfigID string
	TenantID         string
}

// GitHubLoginHandler serves GitHub login and callback routes, mirroring
// OIDCLoginHandler's shape.
type GitHubLoginHandler struct {
	Service              GitHubLoginService
	SessionIssuer        *BrowserSessionHandler
	SessionRefreshWindow time.Duration
	// Audit records an identity_authentication governance-audit event for
	// every callback outcome (issue #5601): allowed
	// ("sso_login_authenticated") and denied (a classification such as
	// "org_not_allowed" or "no_grants", carried through
	// SSOLoginDeniedError). Nil is safe — auditing is skipped, matching
	// LocalIdentityHandler.Audit's nil-safe convention.
	Audit GovernanceAuditAppender
}

// RegisteredProviders returns the set of GitHub providers managed by the
// Service, if the Service implements GitHubProviderLister.
func (h *GitHubLoginHandler) RegisteredProviders() []GitHubRegisteredProvider {
	if h == nil || h.Service == nil {
		return nil
	}
	lister, ok := h.Service.(GitHubProviderLister)
	if !ok {
		return nil
	}
	return lister.ListGitHubProviderIDs()
}

// Mount registers GitHub login routes.
func (h *GitHubLoginHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/github/login", h.handleStart)
	mux.HandleFunc("GET /api/v0/auth/github/callback", h.handleCallback)
}

func (h *GitHubLoginHandler) handleStart(w http.ResponseWriter, r *http.Request) {
	if !h.serviceReady(w) {
		return
	}
	req := GitHubLoginStartRequest{
		ProviderConfigID: QueryParam(r, "provider_config_id"),
		TenantID:         QueryParam(r, "tenant_id"),
		WorkspaceID:      QueryParam(r, "workspace_id"),
		ReturnToPath:     safeGitHubReturnPath(QueryParam(r, "return_to")),
	}
	start, err := h.Service.StartGitHubLogin(r.Context(), req)
	if err != nil {
		writeGitHubLoginError(w, err)
		return
	}
	redirectURL := strings.TrimSpace(start.RedirectURL)
	if redirectURL == "" {
		WriteError(w, http.StatusInternalServerError, "github login did not return a redirect URL")
		return
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (h *GitHubLoginHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	req := GitHubLoginCompleteRequest{
		State: QueryParam(r, "state"),
		Code:  QueryParam(r, "code"),
	}
	complete, err := h.Service.CompleteGitHubLogin(r.Context(), req)
	if err != nil {
		auditGitHubSSOLogin(r, h.Audit, h.SessionIssuer.now(), err, "", "", "")
		writeGitHubLoginError(w, err)
		return
	}
	auditGitHubSSOLogin(r, h.Audit, h.SessionIssuer.now(), nil, complete.ProviderSubjectID, complete.Auth.TenantID, complete.Auth.WorkspaceID)
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
	returnTo := safeGitHubReturnPath(complete.ReturnToPath)
	if returnTo == "" {
		WriteJSON(w, http.StatusCreated, response)
		return
	}
	http.Redirect(w, r, returnTo, http.StatusSeeOther)
}

func (h *GitHubLoginHandler) ready(w http.ResponseWriter) bool {
	if !h.serviceReady(w) {
		return false
	}
	if h.SessionIssuer == nil || h.SessionIssuer.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "browser session store is unavailable")
		return false
	}
	return true
}

func (h *GitHubLoginHandler) serviceReady(w http.ResponseWriter) bool {
	if h == nil || h.Service == nil {
		WriteError(w, http.StatusServiceUnavailable, "github login is unavailable")
		return false
	}
	return true
}

func (h *GitHubLoginHandler) sessionRefreshWindow() time.Duration {
	if h.SessionRefreshWindow > 0 {
		return h.SessionRefreshWindow
	}
	return DefaultGitHubSessionRefreshWindow
}

func writeGitHubLoginError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrGitHubLoginUnavailable):
		WriteError(w, http.StatusServiceUnavailable, "github login is unavailable")
	case errors.Is(err, ErrGitHubLoginInvalidRequest):
		WriteError(w, http.StatusBadRequest, "invalid github login request")
	case errors.Is(err, ErrGitHubLoginDenied):
		WriteError(w, http.StatusForbidden, "github login denied")
	default:
		WriteError(w, http.StatusInternalServerError, "github login failed")
	}
}

func safeGitHubReturnPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return ""
	}
	if strings.ContainsAny(path, "\r\n\t") {
		return ""
	}
	return path
}
