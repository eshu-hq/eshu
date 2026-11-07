// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/authsafe"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

// GitHub sign-in errors live in auth (D13, #9669); these aliases keep every
// existing reference working, including errors.Is identity: each alias holds
// the same sentinel value, so the 503/400/403 mapping is unchanged.
var (
	ErrGitHubLoginUnavailable    = auth.ErrGitHubLoginUnavailable
	ErrGitHubLoginInvalidRequest = auth.ErrGitHubLoginInvalidRequest
	ErrGitHubLoginDenied         = auth.ErrGitHubLoginDenied
)

// Fixed messages for GitHub login failures the sentinels above do not classify
// (#7674), answered through tracing.WriteServerFailure: 500 with the cause on
// the request span, or 499 on a client cancel. The cause can quote an IdP
// response or provider config, so it never reaches the body.
const (
	githubLoginFailedMessage     = "github login failed"
	githubLoginNoRedirectMessage = "github login did not return a redirect URL"
)

// errGitHubLoginRedirectEmpty is the span cause when the login service returned
// an empty redirect URL with no error of its own.
var errGitHubLoginRedirectEmpty = errors.New("github login service returned an empty redirect URL")

// The GitHub login admission contract lives in auth (D13, #9669); these
// aliases keep the handler, the frozen method-set test, and every dest
// consumer compiling against the identical shape.
const GitHubLoginAdmissionContractVersion = auth.GitHubLoginAdmissionContractVersion

type (
	GitHubLoginAdmission        = auth.GitHubLoginAdmission
	GitHubLoginService          = auth.GitHubLoginService
	GitHubLoginStartRequest     = auth.GitHubLoginStartRequest
	GitHubLoginStartResponse    = auth.GitHubLoginStartResponse
	GitHubLoginCompleteRequest  = auth.GitHubLoginCompleteRequest
	GitHubLoginCompleteResponse = auth.GitHubLoginCompleteResponse
	GitHubProviderLister        = auth.GitHubProviderLister
	GitHubRegisteredProvider    = auth.GitHubRegisteredProvider
)

const DefaultGitHubSessionRefreshWindow = auth.DefaultGitHubSessionRefreshWindow

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
		ReturnToPath:     authsafe.ReturnPath(QueryParam(r, "return_to")),
	}
	start, err := h.Service.StartGitHubLogin(r.Context(), req)
	if err != nil {
		writeGitHubLoginError(w, r, err)
		return
	}
	redirectURL := strings.TrimSpace(start.RedirectURL)
	if redirectURL == "" {
		tracing.WriteServerFailure(w, r, errGitHubLoginRedirectEmpty, http.StatusInternalServerError, githubLoginNoRedirectMessage)
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
		writeGitHubLoginError(w, r, err)
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
	returnTo := authsafe.ReturnPath(complete.ReturnToPath)
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

// writeGitHubLoginError answers a login service error: each sentinel keeps its
// fixed 503, 400, or 403, and any other error answers githubLoginFailedMessage
// through tracing.WriteServerFailure.
func writeGitHubLoginError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrGitHubLoginUnavailable):
		WriteError(w, http.StatusServiceUnavailable, "github login is unavailable")
	case errors.Is(err, ErrGitHubLoginInvalidRequest):
		WriteError(w, http.StatusBadRequest, "invalid github login request")
	case errors.Is(err, ErrGitHubLoginDenied):
		WriteError(w, http.StatusForbidden, "github login denied")
	default:
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, githubLoginFailedMessage)
	}
}
