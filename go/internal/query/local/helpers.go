// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package local

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"golang.org/x/crypto/bcrypt"
)

func (h *IdentityHandler) handleEnableBreakGlass(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireSharedOperator(w, r) {
		return
	}
	var req localIdentityBreakGlassRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, "invalid local identity break-glass request")
		return
	}
	now := h.now()
	code := strings.TrimSpace(req.BreakGlassCode)
	if code == "" {
		code = h.newID()
	}
	window := IdentityBreakGlassWindow{
		RecoveryID:         h.newID(),
		TenantID:           strings.TrimSpace(req.TenantID),
		WorkspaceID:        strings.TrimSpace(req.WorkspaceID),
		SubjectIDHash:      IdentityHash(req.SubjectID),
		BreakGlassCodeHash: IdentityHash(code),
		Status:             "active",
		ReasonCode:         localIdentityDefault(req.ReasonCode, "operator_recovery"),
		PolicyRevisionHash: PolicyRevision(req.TenantID, req.WorkspaceID),
		EnabledAt:          now,
		ExpiresAt:          localIdentityExpiry(req.ExpiresAt, now, 15*time.Minute),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := h.Store.EnableLocalIdentityBreakGlass(r.Context(), window); err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeBreakGlass, governanceaudit.DecisionDenied, "break_glass_enable_failed", window.SubjectIDHash)
		querycontract.WriteError(w, http.StatusBadRequest, "failed to enable local identity break-glass")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeBreakGlass, governanceaudit.DecisionAllowed, "break_glass_enabled", window.SubjectIDHash)
	querycontract.WriteJSON(w, http.StatusCreated, map[string]any{
		"recovery_id":          window.RecoveryID,
		"break_glass_code":     code,
		"expires_at":           window.ExpiresAt,
		"policy_revision_hash": window.PolicyRevisionHash,
	})
}

func (h *IdentityHandler) handleBreakGlassSession(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	var req localIdentityBreakGlassSessionRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, "invalid local identity break-glass session request")
		return
	}
	auth, err := h.Store.ResolveLocalIdentityBreakGlass(r.Context(), IdentityBreakGlassAttempt{
		BreakGlassCodeHash: IdentityHash(req.BreakGlassCode),
		Now:                h.now(),
	})
	if err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeBreakGlass, governanceaudit.DecisionDenied, "break_glass_unavailable", "")
		querycontract.WriteError(w, http.StatusUnauthorized, "local identity break-glass unavailable")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeBreakGlass, governanceaudit.DecisionAllowed, "break_glass_session_created", auth.SubjectIDHash)
	h.issueLocalIdentitySession(w, r, auth, "break_glass_authenticated", time.Time{})
}

func (h *IdentityHandler) issueLocalIdentitySession(
	w http.ResponseWriter,
	r *http.Request,
	auth IdentityAuthContext,
	status string,
	lockedUntil time.Time,
) {
	if h.Sessions == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "browser session store is unavailable")
		return
	}
	// Per-tenant session timeout override (issue #4968, epic #4962): resolved
	// here (not inside IssueSessionCookies, which SetupHandler's
	// first-run wizard session also calls and has no SignInPolicy store) so
	// only production/break-glass login honors a tenant's configured
	// idle/absolute override, falling back to h.idleTimeout()/
	// h.absoluteTimeout() when unset or on a policy-read error.
	idleTimeout, absoluteTimeout := queryauth.ResolveSessionTimeouts(
		r.Context(), h.SignInPolicy, auth.TenantID, h.idleTimeout(), h.absoluteTimeout(),
	)
	issued, ok := IssueSessionCookies(
		w, r, h.Sessions, h.newSecret, h.now(), idleTimeout, absoluteTimeout, h.cookieSecureMode(), auth,
	)
	if !ok {
		return
	}
	querycontract.WriteJSON(w, http.StatusOK, IdentitySessionResponse{
		Status:            status,
		Auth:              issued.Auth,
		CSRFToken:         issued.CSRFToken,
		IdleExpiresAt:     issued.IdleExpiresAt,
		AbsoluteExpiresAt: issued.AbsoluteExpiresAt,
		LockedUntil:       lockedUntil,
	})
}

// SessionIssued carries the fields every local-identity login-style
// response needs after IssueSessionCookies creates the server-side
// session row and sets the session/CSRF cookies.
type SessionIssued struct {
	Auth              queryauth.BrowserSessionAuthResponse
	CSRFToken         string
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
}

// IssueSessionCookies is the session-issuance body shared by
// IdentityHandler (production login, break-glass) and SetupHandler (the
// first-run wizard's final step, #4965): create the hash-only browser
// session row, set the session/CSRF cookies, and return the fields the
// caller's own response envelope needs. On any failure it writes the error
// response itself and returns ok=false, so callers only need to check ok
// before building their response body.
func IssueSessionCookies(
	w http.ResponseWriter,
	r *http.Request,
	sessions queryauth.BrowserSessionStore,
	newSecret func() (string, error),
	now time.Time,
	idleTimeout time.Duration,
	absoluteTimeout time.Duration,
	cookieSecure queryauth.CookieSecureMode,
	auth IdentityAuthContext,
) (SessionIssued, bool) {
	if sessions == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "browser session store is unavailable")
		return SessionIssued{}, false
	}
	sessionSecret, err := newSecret()
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, "failed to create local identity session")
		return SessionIssued{}, false
	}
	csrfSecret, err := newSecret()
	if err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, "failed to create local identity session")
		return SessionIssued{}, false
	}
	idleExpiresAt := now.Add(idleTimeout)
	absoluteExpiresAt := now.Add(absoluteTimeout)
	sessionAuth := queryauth.AuthContext{
		Mode:                         queryauth.AuthModeBrowserSession,
		TenantID:                     auth.TenantID,
		WorkspaceID:                  auth.WorkspaceID,
		SubjectClass:                 auth.SubjectClass,
		SubjectIDHash:                auth.SubjectIDHash,
		PolicyRevisionHash:           auth.PolicyRevisionHash,
		AllScopes:                    auth.AllScopes,
		RoleIDs:                      append([]string(nil), auth.RoleIDs...),
		PermissionCatalogEnforced:    auth.PermissionCatalogEnforced,
		AllowedPermissionFeatures:    append([]string(nil), auth.AllowedPermissionFeatures...),
		AllowedPermissionDataClasses: append([]string(nil), auth.AllowedPermissionDataClasses...),
	}
	if err := sessions.CreateBrowserSession(r.Context(), queryauth.BrowserSessionCreateRecord{
		SessionHash:                  queryauth.BrowserSessionSecretHash(sessionSecret),
		CSRFTokenHash:                queryauth.BrowserSessionSecretHash(csrfSecret),
		TenantID:                     auth.TenantID,
		WorkspaceID:                  auth.WorkspaceID,
		SubjectIDHash:                auth.SubjectIDHash,
		SubjectClass:                 auth.SubjectClass,
		PolicyRevisionHash:           auth.PolicyRevisionHash,
		AllScopes:                    auth.AllScopes,
		RoleIDs:                      append([]string(nil), auth.RoleIDs...),
		PermissionCatalogEnforced:    auth.PermissionCatalogEnforced,
		AllowedPermissionFeatures:    append([]string(nil), auth.AllowedPermissionFeatures...),
		AllowedPermissionDataClasses: append([]string(nil), auth.AllowedPermissionDataClasses...),
		IssuedAt:                     now,
		LastSeenAt:                   now,
		IdleExpiresAt:                idleExpiresAt,
		AbsoluteExpiresAt:            absoluteExpiresAt,
		UpdatedAt:                    now,
	}); err != nil {
		querycontract.WriteError(w, http.StatusInternalServerError, "failed to create local identity session")
		return SessionIssued{}, false
	}
	queryauth.WriteBrowserSessionCookies(w, r, cookieSecure, sessionSecret, csrfSecret, absoluteExpiresAt, int(absoluteTimeout.Seconds()))
	return SessionIssued{
		Auth:              queryauth.BrowserSessionAuthResponseFor(sessionAuth),
		CSRFToken:         csrfSecret,
		IdleExpiresAt:     idleExpiresAt,
		AbsoluteExpiresAt: absoluteExpiresAt,
	}, true
}

func (h *IdentityHandler) writeLocalIdentityUnauthenticated(
	w http.ResponseWriter,
	r *http.Request,
	result IdentityAuthenticationResult,
) {
	switch result.Status {
	case IdentityAuthMFARequired:
		h.auditLocalIdentity(r, governanceaudit.EventTypeMFALifecycle, governanceaudit.DecisionDenied, "mfa_required", "")
		querycontract.WriteJSON(w, http.StatusAccepted, IdentitySessionResponse{Status: string(result.Status)})
	case IdentityAuthMustChangePassword:
		// Distinct from the generic "invalid" fallback below (issue #4976):
		// the password and any required MFA both proved, but the credential
		// must be rotated through POST /api/v0/auth/local/password/rotate
		// before a session is issued. 202 Accepted, no session — mirrors
		// mfa_required's "more proof needed" shape rather than a rejection.
		h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionDenied, "must_change_password", result.Auth.SubjectIDHash)
		querycontract.WriteJSON(w, http.StatusAccepted, IdentitySessionResponse{Status: string(result.Status)})
	case IdentityAuthLocked:
		h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionDenied, "local_login_locked", "")
		querycontract.WriteJSON(w, http.StatusLocked, IdentitySessionResponse{Status: string(result.Status), LockedUntil: result.LockedUntil})
	case IdentityAuthDisabled:
		h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionDenied, "local_identity_disabled", "")
		querycontract.WriteJSON(w, http.StatusForbidden, IdentitySessionResponse{Status: string(result.Status)})
	default:
		h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionDenied, "local_login_invalid", "")
		querycontract.WriteJSON(w, http.StatusUnauthorized, IdentitySessionResponse{Status: string(IdentityAuthInvalid)})
	}
}

func (h *IdentityHandler) ready(w http.ResponseWriter) bool {
	if h == nil || h.Store == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "local identity store is unavailable")
		return false
	}
	return true
}

func (h *IdentityHandler) requireSharedOperator(w http.ResponseWriter, r *http.Request) bool {
	auth, ok := queryauth.AuthContextFromContext(r.Context())
	if !ok || queryauth.NormalizeAuthContext(auth).Mode != queryauth.AuthModeShared {
		querycontract.WriteError(w, http.StatusForbidden, "shared operator authentication is required")
		return false
	}
	return true
}

func (h *IdentityHandler) requireAllScopeAuth(w http.ResponseWriter, r *http.Request) bool {
	auth, ok := queryauth.AuthContextFromContext(r.Context())
	auth = queryauth.NormalizeAuthContext(auth)
	if !ok || !auth.AllScopes {
		querycontract.WriteError(w, http.StatusForbidden, "all-scope admin authentication is required")
		return false
	}
	return true
}

func (h *IdentityHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *IdentityHandler) idleTimeout() time.Duration {
	if h.IdleTimeout > 0 {
		return h.IdleTimeout
	}
	return queryauth.DefaultBrowserSessionIdleTimeout
}

func (h *IdentityHandler) absoluteTimeout() time.Duration {
	if h.AbsoluteTimeout > 0 {
		return h.AbsoluteTimeout
	}
	return queryauth.DefaultBrowserSessionAbsoluteTimeout
}

// cookieSecureMode normalizes h.CookieSecure, defaulting to CookieSecureAuto.
func (h *IdentityHandler) cookieSecureMode() queryauth.CookieSecureMode {
	return queryauth.ParseCookieSecureMode(string(h.CookieSecure))
}

func (h *IdentityHandler) hashPassword(password string) (string, error) {
	cost := h.PasswordCost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func (h *IdentityHandler) newID() string {
	secret, err := h.newSecret()
	if err != nil {
		return ""
	}
	return secret
}

func (h *IdentityHandler) newSecret() (string, error) {
	if h.NewSecret != nil {
		secret, err := h.NewSecret()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(secret), nil
	}
	var bytes [localIdentitySecretBytes]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func (h *IdentityHandler) auditLocalIdentity(
	r *http.Request,
	eventType governanceaudit.EventType,
	decision governanceaudit.Decision,
	reasonCode string,
	actorIDHash string,
) {
	if h.Audit == nil {
		return
	}
	auth, _ := queryauth.AuthContextFromContext(r.Context())
	if actorIDHash == "" {
		actorIDHash = auth.SubjectIDHash
	}
	if actorIDHash == "" {
		actorIDHash = IdentityHash(string(auth.Mode))
	}
	event := governanceaudit.Event{
		Type:        eventType,
		ActorClass:  queryauth.ActorClassForAuth(auth),
		ActorIDHash: actorIDHash,
		ScopeClass:  governanceaudit.ScopeClassAdmin,
		Decision:    decision,
		ReasonCode:  reasonCode,
		OccurredAt:  h.now(),
	}
	_ = h.Audit.Append(r.Context(), []governanceaudit.Event{event})
}

func (h *IdentityHandler) requirePermissionFeature(
	w http.ResponseWriter,
	r *http.Request,
	eventType governanceaudit.EventType,
	capability string,
	feature string,
) bool {
	if queryauth.AllowsPermissionFeature(r.Context(), feature) {
		return true
	}
	h.auditLocalIdentity(r, eventType, governanceaudit.DecisionDenied, "permission_catalog_denied", "")
	querycontract.WritePermissionDenied(w, capability)
	return false
}

// IdentityHash is the single hash-only identity-field implementation for
// this package and for the callers outside it that need the identical
// "sha256:<hex>" convention: go/cmd/api/seed_initial_admin.go,
// go/cmd/api/seed_initial_admin_helpers.go, and
// go/internal/cli/admin/credential.go (all reached through root's
// local_identity_alias.go forwarder). Every hash-only identity field this
// codebase writes or compares (subject_id_hash, profile_handle_hash,
// recovery-code hashes, policy revision hash, password_parameters_hash) MUST
// use this single implementation so the local-identity surface never
// produces two different hashes for the same input.
//
// Before the move (#6642) this was two functions -- an unexported
// localIdentityHash doing the work and an exported IdentityHash forwarding to
// it for the outside callers above. Naming rule 4 would have renamed
// localIdentityHash to IdentityHash too (dropping the package-word stutter),
// which would have collided with the already-exported wrapper of the exact
// same behavior. Since the two were always the same computation, the correct
// resolution is one function, not a second exported name: every internal call
// site in this package now calls IdentityHash directly.
func IdentityHash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// IdentityHashes applies IdentityHash to every value, dropping any that hash
// to empty (a blank input). Its home is this package (#6642); root's
// local_identity_alias.go keeps the pre-move localIdentityHashes spelling as
// an unexported forwarder for setup_mfa_handler.go, the setup family's own
// staying caller.
func IdentityHashes(values []string) []string {
	hashes := make([]string, 0, len(values))
	for _, value := range values {
		hash := IdentityHash(value)
		if hash != "" {
			hashes = append(hashes, hash)
		}
	}
	return hashes
}
