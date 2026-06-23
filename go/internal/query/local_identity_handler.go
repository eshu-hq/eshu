package query

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

const localIdentitySecretBytes = 32

// LocalIdentityHandler serves production local identity bootstrap, login, MFA,
// invitation, disablement, and break-glass routes.
type LocalIdentityHandler struct {
	Store           LocalIdentityStore
	Sessions        BrowserSessionStore
	Audit           GovernanceAuditAppender
	NewSecret       func() (string, error)
	Now             func() time.Time
	PasswordCost    int
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
}

// Mount registers local identity routes.
func (h *LocalIdentityHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/auth/local/bootstrap", h.handleBootstrap)
	mux.HandleFunc("POST /api/v0/auth/local/login", h.handleLogin)
	mux.HandleFunc("POST /api/v0/auth/local/invitations", h.handleCreateInvitation)
	mux.HandleFunc("POST /api/v0/auth/local/invitations/accept", h.handleAcceptInvitation)
	mux.HandleFunc("POST /api/v0/auth/local/users/{user_id}/password", h.handleResetPassword)
	mux.HandleFunc("POST /api/v0/auth/local/users/{user_id}/mfa-reset", h.handleResetMFA)
	mux.HandleFunc("POST /api/v0/auth/local/users/{user_id}/disable", h.handleDisableUser)
	mux.HandleFunc("POST /api/v0/auth/local/break-glass", h.handleEnableBreakGlass)
	mux.HandleFunc("POST /api/v0/auth/local/break-glass/session", h.handleBreakGlassSession)
	h.mountAPITokenRoutes(mux)
}

func (h *LocalIdentityHandler) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireSharedOperator(w, r) {
		return
	}
	var req localIdentityBootstrapRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity bootstrap request")
		return
	}
	now := h.now()
	passwordHash, err := h.hashPassword(req.Password)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity password")
		return
	}
	userID, factorID, err := h.newID(), h.newID(), error(nil)
	if userID == "" || factorID == "" {
		err = errors.New("local identity id generation failed")
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to create local identity bootstrap")
		return
	}
	record := LocalIdentityBootstrapRecord{
		TenantID:               strings.TrimSpace(req.TenantID),
		WorkspaceID:            strings.TrimSpace(req.WorkspaceID),
		UserID:                 userID,
		SubjectIDHash:          localIdentityHash(req.LoginID),
		ProfileHandleHash:      localIdentityHash(req.ProfileHandle),
		PasswordHash:           passwordHash,
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: localIdentityHash("bcrypt"),
		MFAFactorID:            factorID,
		MFAFactorKind:          localIdentityDefault(req.MFAFactorKind, "recovery_code"),
		MFACredentialHandle:    strings.TrimSpace(req.MFACredentialHandle),
		RecoveryCodeHashes:     localIdentityHashes(req.RecoveryCodes),
		PolicyRevisionHash:     localIdentityPolicyRevision(req.TenantID, req.WorkspaceID),
		CreatedAt:              now,
	}
	if err := h.Store.BootstrapLocalIdentity(r.Context(), record); err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeBootstrap, governanceaudit.DecisionDenied, "bootstrap_failed", "")
		WriteError(w, http.StatusBadRequest, "failed to bootstrap local identity")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeBootstrap, governanceaudit.DecisionAllowed, "bootstrap_owner_created", record.SubjectIDHash)
	WriteJSON(w, http.StatusCreated, map[string]any{
		"status":       "bootstrapped",
		"user_id":      record.UserID,
		"tenant_id":    record.TenantID,
		"workspace_id": record.WorkspaceID,
	})
}

func (h *LocalIdentityHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	var req localIdentityLoginRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity login request")
		return
	}
	now := h.now()
	result, err := h.Store.AuthenticateLocalIdentity(r.Context(), LocalIdentityAuthenticationAttempt{
		SubjectIDHash:         localIdentityHash(req.LoginID),
		Password:              req.Password,
		MFARecoveryCodeHash:   localIdentityHash(req.RecoveryCode),
		ConsumeRecoveryCodeAt: now,
		Now:                   now,
	})
	if err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionDenied, "local_login_error", "")
		WriteError(w, http.StatusUnauthorized, "local identity authentication failed")
		return
	}
	if !result.Authenticated {
		h.writeLocalIdentityUnauthenticated(w, r, result)
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionAllowed, "local_login_authenticated", result.Auth.SubjectIDHash)
	h.issueLocalIdentitySession(w, r, result.Auth, string(result.Status), result.LockedUntil)
}

func (h *LocalIdentityHandler) handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireAllScopeAuth(w, r) {
		return
	}
	var req localIdentityInvitationRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity invitation request")
		return
	}
	now := h.now()
	inviteCode := strings.TrimSpace(req.InviteCode)
	if inviteCode == "" {
		inviteCode = h.newID()
	}
	tenantID := localIdentityDefault(req.TenantID, authTenantID(r))
	workspaceID := localIdentityDefault(req.WorkspaceID, authWorkspaceID(r))
	record := LocalIdentityInvitationRecord{
		InviteID:             h.newID(),
		TenantID:             tenantID,
		WorkspaceID:          workspaceID,
		InviteCodeHash:       localIdentityHash(inviteCode),
		InviteeHandleHash:    localIdentityHash(req.InviteeHandle),
		InviterSubjectIDHash: authSubjectIDHash(r),
		RoleID:               localIdentityDefault(req.RoleID, "developer"),
		Status:               "active",
		PolicyRevisionHash:   localIdentityPolicyRevision(tenantID, workspaceID),
		ExpiresAt:            localIdentityExpiry(req.ExpiresAt, now, 24*time.Hour),
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if record.InviteID == "" || inviteCode == "" {
		WriteError(w, http.StatusInternalServerError, "failed to create local identity invitation")
		return
	}
	if err := h.Store.CreateLocalIdentityInvitation(r.Context(), record); err != nil {
		WriteError(w, http.StatusBadRequest, "failed to create local identity invitation")
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{
		"invite_id":   record.InviteID,
		"invite_code": inviteCode,
		"expires_at":  record.ExpiresAt,
	})
}

func (h *LocalIdentityHandler) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	var req localIdentityAcceptInvitationRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity invitation acceptance request")
		return
	}
	passwordHash, err := h.hashPassword(req.Password)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity password")
		return
	}
	acceptance := LocalIdentityInvitationAcceptance{
		InviteCodeHash:         localIdentityHash(req.InviteCode),
		UserID:                 h.newID(),
		SubjectIDHash:          localIdentityHash(req.LoginID),
		ProfileHandleHash:      localIdentityHash(req.ProfileHandle),
		PasswordHash:           passwordHash,
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: localIdentityHash("bcrypt"),
		MFAFactorID:            localIdentityOptionalID(h, len(req.RecoveryCodes) > 0),
		MFAFactorKind:          localIdentityDefault(req.MFAFactorKind, "recovery_code"),
		MFACredentialHandle:    strings.TrimSpace(req.MFACredentialHandle),
		RecoveryCodeHashes:     localIdentityHashes(req.RecoveryCodes),
		AcceptedAt:             h.now(),
	}
	if err := h.Store.AcceptLocalIdentityInvitation(r.Context(), acceptance); err != nil {
		WriteError(w, http.StatusBadRequest, "failed to accept local identity invitation")
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{"status": "accepted", "user_id": acceptance.UserID})
}

func (h *LocalIdentityHandler) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireAllScopeAuth(w, r) {
		return
	}
	var req localIdentityPasswordResetRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity password reset request")
		return
	}
	passwordHash, err := h.hashPassword(req.Password)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity password")
		return
	}
	reset := LocalIdentityPasswordReset{
		UserID:                 PathParam(r, "user_id"),
		CredentialID:           h.newID(),
		PasswordHash:           passwordHash,
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: localIdentityHash("bcrypt"),
		ResetAt:                h.now(),
	}
	if err := h.Store.ResetLocalIdentityPassword(r.Context(), reset); err != nil {
		WriteError(w, http.StatusBadRequest, "failed to reset local identity password")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionAllowed, "local_password_reset", "")
	w.WriteHeader(http.StatusNoContent)
}

func (h *LocalIdentityHandler) handleResetMFA(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireAllScopeAuth(w, r) {
		return
	}
	var req localIdentityMFAResetRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity mfa reset request")
		return
	}
	reset := LocalIdentityMFAReset{
		UserID:              PathParam(r, "user_id"),
		MFAFactorID:         h.newID(),
		MFAFactorKind:       localIdentityDefault(req.MFAFactorKind, "recovery_code"),
		MFACredentialHandle: strings.TrimSpace(req.MFACredentialHandle),
		RecoveryCodeHashes:  localIdentityHashes(req.RecoveryCodes),
		ResetAt:             h.now(),
	}
	if err := h.Store.ResetLocalIdentityMFA(r.Context(), reset); err != nil {
		WriteError(w, http.StatusBadRequest, "failed to reset local identity mfa")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeMFALifecycle, governanceaudit.DecisionAllowed, "local_mfa_reset", "")
	w.WriteHeader(http.StatusNoContent)
}

func (h *LocalIdentityHandler) handleDisableUser(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireAllScopeAuth(w, r) {
		return
	}
	if err := h.Store.DisableLocalIdentityUser(r.Context(), LocalIdentityDisableUser{
		UserID:     PathParam(r, "user_id"),
		DisabledAt: h.now(),
	}); err != nil {
		WriteError(w, http.StatusBadRequest, "failed to disable local identity user")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionAllowed, "local_identity_disabled", "")
	w.WriteHeader(http.StatusNoContent)
}
