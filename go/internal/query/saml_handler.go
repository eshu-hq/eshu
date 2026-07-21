// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/samlauth"
)

const (
	maxSAMLACSBodyBytes = 1 << 20
	// DefaultSAMLRequestTTL bounds RelayState and AuthnRequest replay windows.
	DefaultSAMLRequestTTL = 5 * time.Minute
)

// SAMLStore resolves SAML provider state and maps validated principals to Eshu
// authorization context. Implementations must keep raw assertions and claims
// out of durable storage.
type SAMLStore interface {
	GetSAMLProvider(context.Context, string) (SAMLProviderConfig, bool, error)
	CreateSAMLRequest(context.Context, string, SAMLRequestCreateRecord) error
	// ConsumeSAMLRequest atomically marks the pending AuthnRequest as consumed
	// and returns the sanitized return_to path stored when the request was
	// created (empty string when none was stored), whether the request was
	// found and consumed, and any error.
	ConsumeSAMLRequest(context.Context, string, string, string, time.Time) (string, bool, error)
	ReserveSAMLReplay(context.Context, string, string, time.Time) (bool, error)
	ResolveSAMLPrincipal(context.Context, string, samlauth.Principal, time.Time) (AuthContext, bool, error)
}

// SAMLProviderConfig is the query-layer provider view used during login.
type SAMLProviderConfig struct {
	ProviderConfigID                 string
	ServiceProvider                  samlauth.ServiceProviderConfig
	IdentityProviderMetadataXML      []byte
	ExpectedIdentityProviderEntityID string
	GroupMapping                     samlauth.ClaimMapping
	ClockSkew                        time.Duration
}

// SAMLRequestCreateRecord is the hash-only AuthnRequest state stored for ACS.
// ReturnToPath is the sanitized post-login redirect path (same-origin only,
// already validated by safeOIDCReturnPath). Empty string means no redirect —
// the ACS handler falls back to a JSON session response.
type SAMLRequestCreateRecord struct {
	RequestIDHash  string
	RelayStateHash string
	ReturnToPath   string
	IssuedAt       time.Time
	ExpiresAt      time.Time
}

// SAMLAssertion is the validated assertion material needed after XML checks.
type SAMLAssertion struct {
	ResponseID  string
	AssertionID string
	Claims      samlauth.AssertionClaims
	Window      samlauth.AssertionWindow
}

// SAMLAssertionVerifier validates SAML responses before Eshu session creation.
type SAMLAssertionVerifier interface {
	VerifySAMLResponse(context.Context, SAMLProviderConfig, string, []string) (SAMLAssertion, error)
}

// SAMLAuthnRequest is an IdP redirect plus the request ID it carries.
type SAMLAuthnRequest struct {
	RequestID   string
	RedirectURL string
}

// SAMLAuthnRequestBuilder builds SP-initiated SAML login redirects.
type SAMLAuthnRequestBuilder interface {
	BuildSAMLRedirect(context.Context, SAMLProviderConfig, string) (SAMLAuthnRequest, error)
}

// SAMLProviderIDLister is an optional extension on SAMLStore implementations
// that can enumerate the set of provider_config_ids they manage. The
// AuthProviderListHandler uses it to surface env-config SAML providers for the
// pre-auth provider discovery endpoint. Implementations that do not list
// providers (e.g. fakes in unit tests) need not implement this interface.
type SAMLProviderIDLister interface {
	ListProviderIDs() []string
}

// SAMLHandler serves SAML metadata and ACS routes for dashboard SSO.
type SAMLHandler struct {
	Store           SAMLStore
	Sessions        BrowserSessionStore
	Verifier        SAMLAssertionVerifier
	RequestBuilder  SAMLAuthnRequestBuilder
	NewSecret       func() (string, error)
	Now             func() time.Time
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
	// CookieSecure selects the Secure-attribute policy for issued session
	// and CSRF cookies. Empty defaults to CookieSecureAuto (#4964).
	CookieSecure CookieSecureMode
	// SignInPolicy resolves a per-tenant idle/absolute session timeout
	// override (issue #4968, epic #4962). Nil falls back to
	// IdleTimeout/AbsoluteTimeout for every tenant, matching pre-#4968
	// behavior.
	SignInPolicy SignInPolicyReadStore
	// Audit records an identity_authentication governance-audit event for
	// every ACS outcome (issue #5601): allowed ("sso_login_authenticated")
	// and denied (a classification such as "assertion_invalid" or
	// "no_grants"). Nil is safe — auditing is skipped, matching
	// LocalIdentityHandler.Audit's nil-safe convention.
	Audit GovernanceAuditAppender
}

// RegisteredProviderIDs returns the set of provider_config_ids managed by the
// Store, if the Store implements SAMLProviderIDLister. Returns nil when the
// Store does not implement the interface or when h is nil.
func (h *SAMLHandler) RegisteredProviderIDs() []string {
	if h == nil || h.Store == nil {
		return nil
	}
	lister, ok := h.Store.(SAMLProviderIDLister)
	if !ok {
		return nil
	}
	return lister.ListProviderIDs()
}

// Mount registers the SAML service-provider routes.
func (h *SAMLHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/saml/providers/{provider_id}/metadata", h.handleMetadata)
	mux.HandleFunc("GET /api/v0/auth/saml/providers/{provider_id}/login", h.handleLogin)
	mux.HandleFunc("POST /api/v0/auth/saml/providers/{provider_id}/acs", h.handleACS)
}

func (h *SAMLHandler) handleMetadata(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "saml provider store is not configured")
		return
	}
	provider, ok, err := h.Store.GetSAMLProvider(r.Context(), PathParam(r, "provider_id"))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to load saml provider")
		return
	}
	if !ok {
		WriteError(w, http.StatusNotFound, "saml provider not found")
		return
	}
	body, err := samlauth.RenderServiceProviderMetadata(provider.ServiceProvider)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to render saml metadata")
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func (h *SAMLHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "saml provider store is not configured")
		return
	}
	providerID := PathParam(r, "provider_id")
	provider, ok, err := h.Store.GetSAMLProvider(r.Context(), providerID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to load saml provider")
		return
	}
	if !ok {
		WriteError(w, http.StatusNotFound, "saml provider not found")
		return
	}
	relayState, err := h.newSecret()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to create saml request")
		return
	}
	request, err := h.requestBuilder().BuildSAMLRedirect(r.Context(), provider, relayState)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to build saml request")
		return
	}
	now := h.now()
	requestIDHash := BrowserSessionSecretHash(request.RequestID)
	record := SAMLRequestCreateRecord{
		RequestIDHash:  requestIDHash,
		RelayStateHash: BrowserSessionSecretHash(relayState),
		ReturnToPath:   safeOIDCReturnPath(r.URL.Query().Get("return_to")),
		IssuedAt:       now,
		ExpiresAt:      now.Add(DefaultSAMLRequestTTL),
	}
	if record.RequestIDHash == "" || record.RelayStateHash == "" {
		WriteError(w, http.StatusInternalServerError, "failed to create saml request")
		return
	}
	if err := h.Store.CreateSAMLRequest(r.Context(), provider.ProviderConfigID, record); err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to persist saml request")
		return
	}
	http.Redirect(w, r, request.RedirectURL, http.StatusFound)
}

func (h *SAMLHandler) handleACS(w http.ResponseWriter, r *http.Request) {
	if !h.readyForACS(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSAMLACSBodyBytes)
	if err := r.ParseForm(); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid saml response form")
		return
	}
	relayState := strings.TrimSpace(r.PostForm.Get("RelayState"))
	samlResponse := strings.TrimSpace(r.PostForm.Get("SAMLResponse"))
	if relayState == "" || samlResponse == "" {
		WriteError(w, http.StatusBadRequest, "RelayState and SAMLResponse are required")
		return
	}

	now := h.now()
	providerID := PathParam(r, "provider_id")
	provider, ok, err := h.Store.GetSAMLProvider(r.Context(), providerID)
	if err != nil {
		h.auditSAMLLogin(r, now, governanceaudit.DecisionUnavailable, "provider_lookup_error")
		WriteError(w, http.StatusInternalServerError, "failed to load saml provider")
		return
	}
	if !ok {
		h.auditSAMLLogin(r, now, governanceaudit.DecisionDenied, "provider_not_found")
		unauthorizedResponse(w, r)
		return
	}
	requestID, err := requestIDFromSAMLResponse(samlResponse)
	if err != nil {
		h.auditSAMLLogin(r, now, governanceaudit.DecisionDenied, "malformed_response")
		unauthorizedResponse(w, r)
		return
	}
	requestIDHash := BrowserSessionSecretHash(requestID)
	returnToPath, requestConsumed, err := h.Store.ConsumeSAMLRequest(
		r.Context(),
		provider.ProviderConfigID,
		requestIDHash,
		BrowserSessionSecretHash(relayState),
		now,
	)
	if err != nil {
		h.auditSAMLLogin(r, now, governanceaudit.DecisionUnavailable, "request_consume_error")
		WriteError(w, http.StatusInternalServerError, "failed to consume saml request")
		return
	}
	if !requestConsumed {
		h.auditSAMLLogin(r, now, governanceaudit.DecisionDenied, "request_not_found")
		unauthorizedResponse(w, r)
		return
	}

	assertion, err := h.Verifier.VerifySAMLResponse(
		r.Context(),
		provider,
		samlResponse,
		[]string{requestIDHash},
	)
	if err != nil {
		h.auditSAMLLogin(r, now, governanceaudit.DecisionDenied, "assertion_invalid")
		unauthorizedResponse(w, r)
		return
	}
	if err := samlauth.ValidateAssertionWindow(now, assertion.Window); err != nil {
		h.auditSAMLLogin(r, now, governanceaudit.DecisionDenied, "assertion_expired")
		unauthorizedResponse(w, r)
		return
	}
	principal, err := samlauth.NormalizeClaims(assertion.Claims, provider.GroupMapping)
	if err != nil {
		h.auditSAMLLogin(r, now, governanceaudit.DecisionDenied, "claims_invalid")
		unauthorizedResponse(w, r)
		return
	}
	replayHash, err := samlauth.ReplayFingerprint(samlauth.ReplayInput{
		ProviderConfigID: provider.ProviderConfigID,
		RequestID:        requestIDHash,
		ResponseID:       assertion.ResponseID,
		AssertionID:      assertion.AssertionID,
	})
	if err != nil {
		h.auditSAMLLogin(r, now, governanceaudit.DecisionDenied, "replay_fingerprint_error")
		unauthorizedResponse(w, r)
		return
	}
	replayExpiresAt := assertion.Window.NotOnOrAfter
	if replayExpiresAt.IsZero() {
		replayExpiresAt = now.Add(h.absoluteTimeout())
	}
	replayReserved, err := h.Store.ReserveSAMLReplay(
		r.Context(),
		provider.ProviderConfigID,
		replayHash,
		replayExpiresAt,
	)
	if err != nil {
		h.auditSAMLLogin(r, now, governanceaudit.DecisionUnavailable, "replay_reserve_error")
		WriteError(w, http.StatusInternalServerError, "failed to reserve saml replay key")
		return
	}
	if !replayReserved {
		h.auditSAMLLogin(r, now, governanceaudit.DecisionDenied, "replay_detected")
		unauthorizedResponse(w, r)
		return
	}
	auth, ok, err := h.Store.ResolveSAMLPrincipal(
		r.Context(),
		provider.ProviderConfigID,
		principal,
		now,
	)
	if err != nil {
		h.auditSAMLLogin(r, now, governanceaudit.DecisionUnavailable, "principal_resolve_error")
		WriteError(w, http.StatusInternalServerError, "failed to resolve saml principal")
		return
	}
	if !ok {
		h.auditSAMLLogin(r, now, governanceaudit.DecisionDenied, "no_grants")
		unauthorizedResponse(w, r)
		return
	}
	recordSSOLoginAuthentication(r, h.Audit, now, governanceaudit.DecisionAllowed, "sso_login_authenticated", auth.SubjectIDHash)
	h.createSession(w, r, auth, returnToPath, now)
}

// auditSAMLLogin records one identity_authentication governance-audit event
// for a denied or infra-unavailable ACS outcome (issue #5601). Every branch
// that calls this runs before ResolveSAMLPrincipal returns a verified
// identity, so there is never a subject hash to attach — mirroring
// recordSSOLoginAuthentication's GitHub/OIDC denial branches, which are
// unconditionally pre-identity for the same reason.
func (h *SAMLHandler) auditSAMLLogin(r *http.Request, now time.Time, decision governanceaudit.Decision, reasonCode string) {
	recordSSOLoginAuthentication(r, h.Audit, now, decision, reasonCode, "")
}

func (h *SAMLHandler) readyForACS(w http.ResponseWriter) bool {
	if h.Store == nil || h.Sessions == nil || h.Verifier == nil {
		WriteError(w, http.StatusServiceUnavailable, "saml login is not configured")
		return false
	}
	return true
}

func (h *SAMLHandler) createSession(w http.ResponseWriter, r *http.Request, auth AuthContext, returnToPath string, now time.Time) {
	auth = normalizeAuthContext(auth)
	if strings.TrimSpace(auth.TenantID) == "" || strings.TrimSpace(auth.WorkspaceID) == "" {
		WriteError(w, http.StatusInternalServerError, "saml principal resolved without tenant or workspace")
		return
	}
	sessionSecret, err := h.newSecret()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to create browser session")
		return
	}
	csrfSecret, err := h.newSecret()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to create browser session")
		return
	}
	idleTimeout, absoluteTimeout := resolveSessionTimeouts(
		r.Context(), h.SignInPolicy, auth.TenantID, h.idleTimeout(), h.absoluteTimeout(),
	)
	idleExpiresAt := now.Add(idleTimeout)
	absoluteExpiresAt := now.Add(absoluteTimeout)
	if err := h.Sessions.CreateBrowserSession(r.Context(), BrowserSessionCreateRecord{
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
		IssuedAt:                     now,
		LastSeenAt:                   now,
		IdleExpiresAt:                idleExpiresAt,
		AbsoluteExpiresAt:            absoluteExpiresAt,
		UpdatedAt:                    now,
	}); err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to create browser session")
		return
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
	// Mirror OIDC: redirect to returnToPath when a safe same-origin path was
	// stored with the AuthnRequest. Fall back to JSON session response when no
	// path was stored (API clients or direct ACS callers without a console).
	safePath := safeOIDCReturnPath(returnToPath)
	if safePath != "" {
		http.Redirect(w, r, safePath, http.StatusSeeOther)
		return
	}
	WriteJSON(w, http.StatusCreated, BrowserSessionResponse{
		Auth:              browserSessionAuthResponse(sessionAuth),
		CSRFToken:         csrfSecret,
		IdleExpiresAt:     idleExpiresAt,
		AbsoluteExpiresAt: absoluteExpiresAt,
	})
}

func (h *SAMLHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *SAMLHandler) idleTimeout() time.Duration {
	if h.IdleTimeout > 0 {
		return h.IdleTimeout
	}
	return DefaultBrowserSessionIdleTimeout
}

func (h *SAMLHandler) absoluteTimeout() time.Duration {
	if h.AbsoluteTimeout > 0 {
		return h.AbsoluteTimeout
	}
	return DefaultBrowserSessionAbsoluteTimeout
}

// cookieSecureMode normalizes h.CookieSecure, defaulting to CookieSecureAuto.
func (h *SAMLHandler) cookieSecureMode() CookieSecureMode {
	return ParseCookieSecureMode(string(h.CookieSecure))
}

func (h *SAMLHandler) newSecret() (string, error) {
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

func (h *SAMLHandler) requestBuilder() SAMLAuthnRequestBuilder {
	if h.RequestBuilder != nil {
		return h.RequestBuilder
	}
	return CrewjamSAMLRequestBuilder{}
}
