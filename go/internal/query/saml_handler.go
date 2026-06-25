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
		WriteError(w, http.StatusInternalServerError, "failed to load saml provider")
		return
	}
	if !ok {
		unauthorizedResponse(w, r)
		return
	}
	requestID, err := requestIDFromSAMLResponse(samlResponse)
	if err != nil {
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
		WriteError(w, http.StatusInternalServerError, "failed to consume saml request")
		return
	}
	if !requestConsumed {
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
		unauthorizedResponse(w, r)
		return
	}
	if err := samlauth.ValidateAssertionWindow(now, assertion.Window); err != nil {
		unauthorizedResponse(w, r)
		return
	}
	principal, err := samlauth.NormalizeClaims(assertion.Claims, provider.GroupMapping)
	if err != nil {
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
		WriteError(w, http.StatusInternalServerError, "failed to reserve saml replay key")
		return
	}
	if !replayReserved {
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
		WriteError(w, http.StatusInternalServerError, "failed to resolve saml principal")
		return
	}
	if !ok {
		unauthorizedResponse(w, r)
		return
	}
	h.createSession(w, r, auth, returnToPath, now)
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
	idleExpiresAt := now.Add(h.idleTimeout())
	absoluteExpiresAt := now.Add(h.absoluteTimeout())
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
		sessionSecret,
		csrfSecret,
		absoluteExpiresAt,
		int(h.absoluteTimeout().Seconds()),
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
