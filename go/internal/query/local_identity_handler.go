// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const localIdentitySecretBytes = 32

// LocalIdentityHandler serves production local identity bootstrap, login, MFA,
// invitation, disablement, break-glass, and profile read routes.
type LocalIdentityHandler struct {
	Store           LocalIdentityProfileLister
	Sessions        BrowserSessionStore
	Audit           GovernanceAuditAppender
	NewSecret       func() (string, error)
	Now             func() time.Time
	PasswordCost    int
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
	// CookieSecure selects the Secure-attribute policy for issued session
	// and CSRF cookies. Empty defaults to CookieSecureAuto (#4964).
	CookieSecure CookieSecureMode
	// SignInPolicy reads the tenant sign-in policy (issue #4968, epic #4962)
	// enforced by handleLogin (require_sso: local login denied for a
	// non-admin identity; break-glass local admin sign-in is unaffected — see
	// requireSSODecision in local_identity_sign_in_policy_gate.go) and by
	// handleCreateInvitation (allow_local_user_creation). Nil means no
	// policy store is wired: every gate fails open to today's unrestricted
	// behavior, matching this package's existing store-unavailable-fails-
	// open convention for optional reads.
	SignInPolicy SignInPolicyReadStore
	// Instruments records the require_sso login-gate decision
	// (eshu_dp_auth_require_sso_login_gate_total). Optional: nil-safe.
	Instruments *telemetry.Instruments
}

// Mount registers local identity routes.
func (h *LocalIdentityHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/auth/local/bootstrap", h.handleBootstrap)
	mux.HandleFunc("POST /api/v0/auth/local/login", h.handleLogin)
	mux.HandleFunc("POST /api/v0/auth/local/invitations", h.handleCreateInvitation)
	mux.HandleFunc("POST /api/v0/auth/local/invitations/accept", h.handleAcceptInvitation)
	mux.HandleFunc("POST /api/v0/auth/local/users/{user_id}/password", h.handleResetPassword)
	mux.HandleFunc("POST /api/v0/auth/local/password/rotate", h.handleRotatePassword)
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
	// require_sso must take precedence over an mfa_required response for a
	// non-admin (issue #5001 P2 review finding, PR #5049 Codex review):
	// AuthenticateLocalIdentity now returns mfa_required (not an error, no
	// session) for a non-admin missing MFA under require_mfa_for_all_users,
	// but when require_sso is ALSO on for this tenant that non-admin can
	// never complete the MFA challenge through local login — require_sso
	// already forbids issuing them a local session at all. This converts
	// that doomed 202 mfa_required into the correct 403 require_sso denial
	// instead of inviting an MFA proof attempt that could never succeed.
	// requireSSODecision below remains the SINGLE authoritative require_sso
	// read (called here instead of only after the Authenticated branch, not
	// in addition to it), so there is no second read and no TOCTOU window.
	// Store-side MFA enforcement is never skipped or altered based on
	// require_sso — AuthenticateLocalIdentity is still the sole MFA
	// authority. Admins (AllScopes=true) are excluded so an admin missing
	// MFA still gets the ordinary 202 mfa_required break-glass challenge.
	if result.Status == LocalIdentityAuthMFARequired && !result.Auth.AllScopes && result.Auth.TenantID != "" {
		if allowed, decision := h.requireSSODecision(r.Context(), result.Auth); !allowed {
			h.recordRequireSSOLoginGate(r.Context(), decision)
			h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionDenied, "local_login_denied_require_sso_policy", result.Auth.SubjectIDHash)
			WriteError(w, http.StatusForbidden, "local sign-in is disabled by tenant policy; sign in with SSO, or an admin may use break-glass local sign-in")
			return
		}
	}
	if !result.Authenticated {
		h.writeLocalIdentityUnauthenticated(w, r, result)
		return
	}
	// Sign-in policy require_sso gate (issue #4968). Applied identically
	// regardless of how the caller reached this endpoint: there is no request
	// field that bypasses it. The console's /login?local=1 is a pure UI hint
	// (render the local form even when the policy would otherwise hide it) —
	// it carries no server-side meaning, so the guardrail cannot be
	// client-bypassed. Break-glass is simply "the identity that just
	// authenticated is an admin."
	allowed, decision := h.requireSSODecision(r.Context(), result.Auth)
	h.recordRequireSSOLoginGate(r.Context(), decision)
	if !allowed {
		h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionDenied, "local_login_denied_require_sso_policy", result.Auth.SubjectIDHash)
		WriteError(w, http.StatusForbidden, "local sign-in is disabled by tenant policy; sign in with SSO, or an admin may use break-glass local sign-in")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionAllowed, "local_login_authenticated", result.Auth.SubjectIDHash)
	h.issueLocalIdentitySession(w, r, result.Auth, string(result.Status), result.LockedUntil)
}

func (h *LocalIdentityHandler) handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireAllScopeAuth(w, r) {
		return
	}
	if !h.requirePermissionFeature(
		w,
		r,
		governanceaudit.EventTypeRoleGrantChange,
		"identity_admin.invitation_create",
		permissionFeatureIdentityAdmin,
	) {
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
	// Sign-in policy allow_local_user_creation gate (issue #4968). SSO login
	// never creates an invitation or a local identity row — it resolves an
	// ephemeral session directly from IdP claims/group mappings (see
	// go/internal/oidclogin.Service.CompleteOIDCLogin and
	// SAMLHandler.handleACS) — so this gate needs no SSO-identity carve-out:
	// invitations are exclusively the local-password-account creation path.
	if !h.allowLocalUserCreation(r.Context(), tenantID) {
		h.auditLocalIdentity(r, governanceaudit.EventTypeRoleGrantChange, governanceaudit.DecisionDenied, "local_invitation_denied_by_policy", "")
		WriteError(w, http.StatusForbidden, "local user creation is disabled by tenant sign-in policy")
		return
	}
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
	if !h.requirePermissionFeature(
		w,
		r,
		governanceaudit.EventTypeIdentityAuthentication,
		"identity_admin.password_reset",
		permissionFeatureIdentityAdmin,
	) {
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

// handleRotatePassword is the self-service forced-rotation surface (issue
// #4976): unlike handleResetPassword (admin-only, requires an existing
// all-scope session), this is public and re-proves the caller's identity
// itself — current_password (and recovery_code, when the account has an
// active MFA factor) — so a must-change-password credential (the
// ESHU_ADMIN_USERNAME/PASSWORD[_FILE]-seeded bootstrap admin) can rotate and
// obtain a session without ever having held one. Any local user may use this
// route to voluntarily rotate their own password; success always clears
// must_change_password regardless of its prior value.
func (h *LocalIdentityHandler) handleRotatePassword(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	var req localIdentityPasswordRotationRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity password rotation request")
		return
	}
	passwordHash, err := h.hashPassword(req.NewPassword)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity password")
		return
	}
	now := h.now()
	result, err := h.Store.RotateLocalIdentityPassword(r.Context(), LocalIdentityPasswordRotation{
		SubjectIDHash:             localIdentityHash(req.LoginID),
		CurrentPassword:           req.CurrentPassword,
		NewPasswordHash:           passwordHash,
		NewPasswordAlgorithm:      "bcrypt",
		NewPasswordParametersHash: localIdentityHash("bcrypt"),
		CredentialID:              h.newID(),
		MFARecoveryCodeHash:       localIdentityHash(req.RecoveryCode),
		ConsumeRecoveryCodeAt:     now,
		Now:                       now,
	})
	if err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionDenied, "local_password_rotation_error", "")
		WriteError(w, http.StatusUnauthorized, "local identity password rotation failed")
		return
	}
	if !result.Authenticated {
		h.writeLocalIdentityUnauthenticated(w, r, result)
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeIdentityAuthentication, governanceaudit.DecisionAllowed, "local_password_rotation_forced", result.Auth.SubjectIDHash)
	h.issueLocalIdentitySession(w, r, result.Auth, string(result.Status), result.LockedUntil)
}

func (h *LocalIdentityHandler) handleResetMFA(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireAllScopeAuth(w, r) {
		return
	}
	if !h.requirePermissionFeature(
		w,
		r,
		governanceaudit.EventTypeMFALifecycle,
		"identity_admin.mfa_reset",
		permissionFeatureIdentityAdmin,
	) {
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
	if !h.requirePermissionFeature(
		w,
		r,
		governanceaudit.EventTypeIdentityAuthentication,
		"identity_admin.user_disable",
		permissionFeatureIdentityAdmin,
	) {
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
