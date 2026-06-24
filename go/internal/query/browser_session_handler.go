package query

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultBrowserSessionIdleTimeout is the dashboard browser session idle window.
	DefaultBrowserSessionIdleTimeout = 30 * time.Minute
	// DefaultBrowserSessionAbsoluteTimeout is the maximum browser session lifetime.
	DefaultBrowserSessionAbsoluteTimeout = 12 * time.Hour
	browserSessionSecretBytes            = 32
)

// BrowserSessionStore is the write surface for server-managed dashboard
// sessions. Implementations must persist only hashed session and CSRF values.
type BrowserSessionStore interface {
	CreateBrowserSession(context.Context, BrowserSessionCreateRecord) error
	RevokeBrowserSession(context.Context, string, time.Time) error
	SwitchBrowserSessionWorkspace(context.Context, string, string, string, time.Time) (AuthContext, bool, error)
}

// BrowserSessionCreateRecord is the hash-only session row requested by the
// HTTP handler.
type BrowserSessionCreateRecord struct {
	SessionHash                  string
	CSRFTokenHash                string
	TenantID                     string
	WorkspaceID                  string
	SubjectIDHash                string
	SubjectClass                 string
	PolicyRevisionHash           string
	RoleIDs                      []string
	AllScopes                    bool
	PermissionCatalogEnforced    bool
	AllowedScopeIDs              []string
	AllowedRepositoryIDs         []string
	AllowedPermissionFeatures    []string
	AllowedPermissionDataClasses []string
	ExternalProviderConfigID     string
	ExternalSubjectIDHash        string
	ExternalGroupHashes          []string
	ExternalAuthValidatedAt      time.Time
	ExternalAuthStaleAfter       time.Time
	IssuedAt                     time.Time
	LastSeenAt                   time.Time
	IdleExpiresAt                time.Time
	AbsoluteExpiresAt            time.Time
	UpdatedAt                    time.Time
}

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
}

// BrowserSessionResponse is returned by browser session routes.
type BrowserSessionResponse struct {
	Auth              BrowserSessionAuthResponse `json:"auth"`
	CSRFToken         string                     `json:"csrf_token,omitempty"`
	IdleExpiresAt     time.Time                  `json:"idle_expires_at,omitempty"`
	AbsoluteExpiresAt time.Time                  `json:"absolute_expires_at,omitempty"`
}

// BrowserSessionAuthResponse is the public JSON view of a request auth context.
type BrowserSessionAuthResponse struct {
	Mode                      AuthMode `json:"mode"`
	TenantID                  string   `json:"tenant_id,omitempty"`
	WorkspaceID               string   `json:"workspace_id,omitempty"`
	SubjectClass              string   `json:"subject_class,omitempty"`
	SubjectIDHash             string   `json:"subject_id_hash,omitempty"`
	PolicyRevisionHash        string   `json:"policy_revision_hash,omitempty"`
	RoleIDs                   []string `json:"role_ids,omitempty"`
	AllScopes                 bool     `json:"all_scopes"`
	AllowedScopeIDs           []string `json:"allowed_scope_ids,omitempty"`
	AllowedRepositoryIDs      []string `json:"allowed_repository_ids,omitempty"`
	PermissionCatalogEnforced bool     `json:"permission_catalog_enforced"`
	AllowedPermissionFeatures []string `json:"allowed_permission_features,omitempty"`
	// ExternalProviderConfigID is the stored OIDC/SAML provider config ID for
	// sessions established via an external IdP. Omitted for local sessions.
	ExternalProviderConfigID string `json:"external_provider_config_id,omitempty"`
}

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
	idleExpiresAt := now.Add(h.idleTimeout())
	absoluteExpiresAt := now.Add(h.absoluteTimeout())
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
		sessionSecret,
		csrfSecret,
		absoluteExpiresAt,
		int(h.absoluteTimeout().Seconds()),
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
	writeBrowserSessionCookies(w, "", "", time.Time{}, -1)
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
	cookie, err := r.Cookie(BrowserSessionCookieName)
	if err != nil {
		return "", false
	}
	sessionHash := BrowserSessionSecretHash(cookie.Value)
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

func writeBrowserSessionCookies(
	w http.ResponseWriter,
	sessionSecret string,
	csrfSecret string,
	expiresAt time.Time,
	maxAge int,
) {
	expires := time.Unix(0, 0).UTC()
	if maxAge > 0 {
		expires = expiresAt.UTC()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     BrowserSessionCookieName,
		Value:    sessionSecret,
		Path:     "/",
		MaxAge:   maxAge,
		Expires:  expires,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     BrowserSessionCSRFCookieName,
		Value:    csrfSecret,
		Path:     "/",
		MaxAge:   maxAge,
		Expires:  expires,
		HttpOnly: false,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

func browserSessionAuthResponse(auth AuthContext) BrowserSessionAuthResponse {
	auth = normalizeBrowserSessionAuthContext(auth)
	return BrowserSessionAuthResponse{
		Mode:                      auth.Mode,
		TenantID:                  auth.TenantID,
		WorkspaceID:               auth.WorkspaceID,
		SubjectClass:              auth.SubjectClass,
		SubjectIDHash:             auth.SubjectIDHash,
		PolicyRevisionHash:        auth.PolicyRevisionHash,
		RoleIDs:                   append([]string(nil), auth.RoleIDs...),
		AllScopes:                 auth.AllScopes,
		AllowedScopeIDs:           append([]string(nil), auth.AllowedScopeIDs...),
		AllowedRepositoryIDs:      append([]string(nil), auth.AllowedRepositoryIDs...),
		PermissionCatalogEnforced: auth.PermissionCatalogEnforced,
		AllowedPermissionFeatures: append([]string(nil), auth.AllowedPermissionFeatures...),
		ExternalProviderConfigID:  auth.ExternalProviderConfigID,
	}
}
