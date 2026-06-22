package query

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"golang.org/x/crypto/bcrypt"
)

func (h *LocalIdentityHandler) handleEnableBreakGlass(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireSharedOperator(w, r) {
		return
	}
	var req localIdentityBreakGlassRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity break-glass request")
		return
	}
	now := h.now()
	code := strings.TrimSpace(req.BreakGlassCode)
	if code == "" {
		code = h.newID()
	}
	window := LocalIdentityBreakGlassWindow{
		RecoveryID:         h.newID(),
		TenantID:           strings.TrimSpace(req.TenantID),
		WorkspaceID:        strings.TrimSpace(req.WorkspaceID),
		SubjectIDHash:      localIdentityHash(req.SubjectID),
		BreakGlassCodeHash: localIdentityHash(code),
		Status:             "active",
		ReasonCode:         localIdentityDefault(req.ReasonCode, "operator_recovery"),
		PolicyRevisionHash: localIdentityPolicyRevision(req.TenantID, req.WorkspaceID),
		EnabledAt:          now,
		ExpiresAt:          localIdentityExpiry(req.ExpiresAt, now, 15*time.Minute),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := h.Store.EnableLocalIdentityBreakGlass(r.Context(), window); err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeBreakGlass, governanceaudit.DecisionDenied, "break_glass_enable_failed", window.SubjectIDHash)
		WriteError(w, http.StatusBadRequest, "failed to enable local identity break-glass")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeBreakGlass, governanceaudit.DecisionAllowed, "break_glass_enabled", window.SubjectIDHash)
	WriteJSON(w, http.StatusCreated, map[string]any{
		"recovery_id":          window.RecoveryID,
		"break_glass_code":     code,
		"expires_at":           window.ExpiresAt,
		"policy_revision_hash": window.PolicyRevisionHash,
	})
}

func (h *LocalIdentityHandler) handleBreakGlassSession(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	var req localIdentityBreakGlassSessionRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity break-glass session request")
		return
	}
	auth, err := h.Store.ResolveLocalIdentityBreakGlass(r.Context(), LocalIdentityBreakGlassAttempt{
		BreakGlassCodeHash: localIdentityHash(req.BreakGlassCode),
		Now:                h.now(),
	})
	if err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeBreakGlass, governanceaudit.DecisionDenied, "break_glass_unavailable", "")
		WriteError(w, http.StatusUnauthorized, "local identity break-glass unavailable")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeBreakGlass, governanceaudit.DecisionAllowed, "break_glass_session_created", auth.SubjectIDHash)
	h.issueLocalIdentitySession(w, r, auth, "break_glass_authenticated", time.Time{})
}

func (h *LocalIdentityHandler) issueLocalIdentitySession(
	w http.ResponseWriter,
	r *http.Request,
	auth LocalIdentityAuthContext,
	status string,
	lockedUntil time.Time,
) {
	if h.Sessions == nil {
		WriteError(w, http.StatusServiceUnavailable, "browser session store is unavailable")
		return
	}
	sessionSecret, err := h.newSecret()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to create local identity session")
		return
	}
	csrfSecret, err := h.newSecret()
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to create local identity session")
		return
	}
	now := h.now()
	idleExpiresAt := now.Add(h.idleTimeout())
	absoluteExpiresAt := now.Add(h.absoluteTimeout())
	sessionAuth := AuthContext{
		Mode:               AuthModeBrowserSession,
		TenantID:           auth.TenantID,
		WorkspaceID:        auth.WorkspaceID,
		SubjectClass:       auth.SubjectClass,
		SubjectIDHash:      auth.SubjectIDHash,
		PolicyRevisionHash: auth.PolicyRevisionHash,
		AllScopes:          auth.AllScopes,
	}
	if err := h.Sessions.CreateBrowserSession(r.Context(), BrowserSessionCreateRecord{
		SessionHash:        BrowserSessionSecretHash(sessionSecret),
		CSRFTokenHash:      BrowserSessionSecretHash(csrfSecret),
		TenantID:           auth.TenantID,
		WorkspaceID:        auth.WorkspaceID,
		SubjectIDHash:      auth.SubjectIDHash,
		SubjectClass:       auth.SubjectClass,
		PolicyRevisionHash: auth.PolicyRevisionHash,
		AllScopes:          auth.AllScopes,
		IssuedAt:           now,
		LastSeenAt:         now,
		IdleExpiresAt:      idleExpiresAt,
		AbsoluteExpiresAt:  absoluteExpiresAt,
		UpdatedAt:          now,
	}); err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to create local identity session")
		return
	}
	writeBrowserSessionCookies(w, sessionSecret, csrfSecret, absoluteExpiresAt, int(h.absoluteTimeout().Seconds()))
	WriteJSON(w, http.StatusOK, LocalIdentitySessionResponse{
		Status:            status,
		Auth:              browserSessionAuthResponse(sessionAuth),
		CSRFToken:         csrfSecret,
		IdleExpiresAt:     idleExpiresAt,
		AbsoluteExpiresAt: absoluteExpiresAt,
		LockedUntil:       lockedUntil,
	})
}

func (h *LocalIdentityHandler) writeLocalIdentityUnauthenticated(
	w http.ResponseWriter,
	r *http.Request,
	result LocalIdentityAuthenticationResult,
) {
	switch result.Status {
	case LocalIdentityAuthMFARequired:
		h.auditLocalIdentity(r, governanceaudit.EventTypeMFALifecycle, governanceaudit.DecisionDenied, "mfa_required", "")
		WriteJSON(w, http.StatusAccepted, LocalIdentitySessionResponse{Status: string(result.Status)})
	case LocalIdentityAuthLocked:
		h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionDenied, "local_login_locked", "")
		WriteJSON(w, http.StatusLocked, LocalIdentitySessionResponse{Status: string(result.Status), LockedUntil: result.LockedUntil})
	case LocalIdentityAuthDisabled:
		h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionDenied, "local_identity_disabled", "")
		WriteJSON(w, http.StatusForbidden, LocalIdentitySessionResponse{Status: string(result.Status)})
	default:
		h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionDenied, "local_login_invalid", "")
		WriteJSON(w, http.StatusUnauthorized, LocalIdentitySessionResponse{Status: string(LocalIdentityAuthInvalid)})
	}
}

func (h *LocalIdentityHandler) ready(w http.ResponseWriter) bool {
	if h == nil || h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "local identity store is unavailable")
		return false
	}
	return true
}

func (h *LocalIdentityHandler) requireSharedOperator(w http.ResponseWriter, r *http.Request) bool {
	auth, ok := AuthContextFromContext(r.Context())
	if !ok || normalizeAuthContext(auth).Mode != AuthModeShared {
		WriteError(w, http.StatusForbidden, "shared operator authentication is required")
		return false
	}
	return true
}

func (h *LocalIdentityHandler) requireAllScopeAuth(w http.ResponseWriter, r *http.Request) bool {
	auth, ok := AuthContextFromContext(r.Context())
	auth = normalizeAuthContext(auth)
	if !ok || !auth.AllScopes {
		WriteError(w, http.StatusForbidden, "all-scope admin authentication is required")
		return false
	}
	return true
}

func (h *LocalIdentityHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *LocalIdentityHandler) idleTimeout() time.Duration {
	if h.IdleTimeout > 0 {
		return h.IdleTimeout
	}
	return DefaultBrowserSessionIdleTimeout
}

func (h *LocalIdentityHandler) absoluteTimeout() time.Duration {
	if h.AbsoluteTimeout > 0 {
		return h.AbsoluteTimeout
	}
	return DefaultBrowserSessionAbsoluteTimeout
}

func (h *LocalIdentityHandler) hashPassword(password string) (string, error) {
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

func (h *LocalIdentityHandler) newID() string {
	secret, err := h.newSecret()
	if err != nil {
		return ""
	}
	return secret
}

func (h *LocalIdentityHandler) newSecret() (string, error) {
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

func (h *LocalIdentityHandler) auditLocalIdentity(
	r *http.Request,
	eventType governanceaudit.EventType,
	decision governanceaudit.Decision,
	reasonCode string,
	actorIDHash string,
) {
	if h.Audit == nil {
		return
	}
	auth, _ := AuthContextFromContext(r.Context())
	if actorIDHash == "" {
		actorIDHash = auth.SubjectIDHash
	}
	if actorIDHash == "" {
		actorIDHash = localIdentityHash(string(auth.Mode))
	}
	event := governanceaudit.Event{
		Type:        eventType,
		ActorClass:  localIdentityActorClass(auth),
		ActorIDHash: actorIDHash,
		ScopeClass:  governanceaudit.ScopeClassAdmin,
		Decision:    decision,
		ReasonCode:  reasonCode,
		OccurredAt:  h.now(),
	}
	_ = h.Audit.Append(r.Context(), []governanceaudit.Event{event})
}

func localIdentityActorClass(auth AuthContext) governanceaudit.ActorClass {
	switch auth.Mode {
	case AuthModeShared:
		return governanceaudit.ActorClassSharedToken
	case AuthModeBrowserSession:
		return governanceaudit.ActorClassOperator
	case AuthModeScoped:
		return governanceaudit.ActorClassScopedToken
	default:
		return governanceaudit.ActorClassAnonymous
	}
}

func localIdentityHash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func localIdentityHashes(values []string) []string {
	hashes := make([]string, 0, len(values))
	for _, value := range values {
		hash := localIdentityHash(value)
		if hash != "" {
			hashes = append(hashes, hash)
		}
	}
	return hashes
}
