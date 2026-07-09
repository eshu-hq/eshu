// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SetupHandler serves the first-run setup wizard routes (epic #4962, issue
// #4965): the pre-auth setup-state check, the bootstrap-credential claim
// step, and the first-administrator password step. The final MFA-enrollment
// step (which also permanently seals the wizard) lives in
// setup_mfa_handler.go to keep both files under the 500-line cap.
//
// Every mutating route re-verifies the submitted bootstrap credential
// against the still-unconsumed sealed envelope on every call (see
// SetupStore's doc comment) instead of minting its own session/ticket state:
// the envelope itself is the continuity proof across the wizard's three
// steps, so there is no additional secret to steal or replay-protect.
type SetupHandler struct {
	Store           SetupStore
	Sessions        BrowserSessionStore
	Audit           GovernanceAuditAppender
	Instruments     *telemetry.Instruments
	NewSecret       func() (string, error)
	Now             func() time.Time
	PasswordCost    int
	IdleTimeout     time.Duration
	AbsoluteTimeout time.Duration
	// CookieSecure selects the Secure-attribute policy for the session cookie
	// issued on wizard completion. Empty defaults to CookieSecureAuto (#4964).
	CookieSecure CookieSecureMode
	// BootstrapMode echoes ESHU_AUTH_BOOTSTRAP_MODE in the setup-state
	// response so the console can render mode-appropriate guidance. It does
	// not gate any behavior here — SetupNeeded's seal signal is the single
	// source of truth for whether the wizard is reachable.
	BootstrapMode string
}

// Mount registers first-run setup wizard routes. auth.go's publicHTTPPaths
// MUST list every route registered here: a fresh deployment has no session,
// bearer token, or prior credential to authenticate with, so these routes
// must bypass AuthMiddleware entirely and rely on their own bootstrap-
// credential proof instead.
func (h *SetupHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/setup-state", h.handleState)
	mux.HandleFunc("POST /api/v0/auth/setup/claim", h.handleClaim)
	mux.HandleFunc("POST /api/v0/auth/setup/admin", h.handleCreateAdmin)
	mux.HandleFunc("POST /api/v0/auth/setup/mfa", h.handleCompleteMFA)
}

func (h *SetupHandler) handleState(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	needed, err := h.Store.SetupNeeded(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to determine setup state")
		return
	}
	WriteJSON(w, http.StatusOK, SetupStateResponse{
		NeedsSetup:    needed,
		BootstrapMode: h.BootstrapMode,
	})
}

func (h *SetupHandler) handleClaim(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireSetupOpen(w, r) {
		return
	}
	var req setupClaimRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid setup claim request")
		return
	}
	ok, err := h.Store.VerifyBootstrapCredential(r.Context(), req.Username, req.Password)
	if err != nil {
		h.auditSetup(r, governanceaudit.DecisionDenied, "setup_claim_error")
		h.recordOutcome(r, "claim_error")
		WriteError(w, http.StatusInternalServerError, "failed to verify bootstrap credential")
		return
	}
	if !ok {
		h.auditSetup(r, governanceaudit.DecisionDenied, "setup_claim_invalid")
		h.recordOutcome(r, "claim_denied")
		WriteError(w, http.StatusUnauthorized, setupInvalidCredentialMessage)
		return
	}
	h.auditSetup(r, governanceaudit.DecisionAllowed, "setup_claimed")
	h.recordOutcome(r, "claim_allowed")
	WriteJSON(w, http.StatusOK, map[string]any{"status": "claimed"})
}

func (h *SetupHandler) handleCreateAdmin(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireSetupOpen(w, r) {
		return
	}
	var req setupAdminRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid setup admin request")
		return
	}
	if strings.TrimSpace(req.NewPassword) == "" {
		WriteError(w, http.StatusBadRequest, "new_password is required")
		return
	}
	owner, ok := h.verifyAndResolveOwner(w, r, req.Username, req.Password, "admin")
	if !ok {
		return
	}
	passwordHash, err := h.hashPassword(req.NewPassword)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid new password")
		return
	}
	now := h.now()
	if err := h.Store.RotateSetupPassword(r.Context(), LocalIdentityPasswordReset{
		UserID:                 owner.UserID,
		CredentialID:           h.newID(),
		PasswordHash:           passwordHash,
		PasswordAlgorithm:      "bcrypt",
		PasswordParametersHash: localIdentityHash("bcrypt"),
		ResetAt:                now,
	}); err != nil {
		h.recordOutcome(r, "admin_error")
		WriteError(w, http.StatusInternalServerError, "failed to set administrator password")
		return
	}
	h.auditSetup(r, governanceaudit.DecisionAllowed, "setup_admin_password_set")
	h.recordOutcome(r, "admin_allowed")
	WriteJSON(w, http.StatusOK, map[string]any{
		"status":       "admin_created",
		"tenant_id":    owner.TenantID,
		"workspace_id": owner.WorkspaceID,
	})
}

// verifyAndResolveOwner reproves the bootstrap credential and resolves its
// owning identity, the shared prelude for the admin and MFA steps. On
// failure it writes the response itself and returns ok=false.
func (h *SetupHandler) verifyAndResolveOwner(
	w http.ResponseWriter,
	r *http.Request,
	username, password, step string,
) (SetupOwner, bool) {
	ok, err := h.Store.VerifyBootstrapCredential(r.Context(), username, password)
	if err != nil {
		h.auditSetup(r, governanceaudit.DecisionDenied, "setup_"+step+"_error")
		h.recordOutcome(r, step+"_error")
		WriteError(w, http.StatusInternalServerError, "failed to verify bootstrap credential")
		return SetupOwner{}, false
	}
	if !ok {
		h.auditSetup(r, governanceaudit.DecisionDenied, "setup_"+step+"_invalid")
		h.recordOutcome(r, step+"_denied")
		WriteError(w, http.StatusUnauthorized, setupInvalidCredentialMessage)
		return SetupOwner{}, false
	}
	owner, err := h.Store.ResolveSetupOwner(r.Context())
	if err != nil {
		h.auditSetup(r, governanceaudit.DecisionDenied, "setup_"+step+"_owner_unresolved")
		h.recordOutcome(r, step+"_error")
		WriteError(w, http.StatusInternalServerError, "failed to resolve setup owner")
		return SetupOwner{}, false
	}
	return owner, true
}

// requireSetupOpen re-checks SetupNeeded on every mutating call and fails
// closed with 410 Gone once it returns false, so the wizard cannot be
// re-entered after completion or after any identity otherwise comes to
// exist (#4965's permanent-sealing requirement).
func (h *SetupHandler) requireSetupOpen(w http.ResponseWriter, r *http.Request) bool {
	needed, err := h.Store.SetupNeeded(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to determine setup state")
		return false
	}
	if !needed {
		WriteError(w, http.StatusGone, "first-run setup is no longer available; this instance already has an identity")
		return false
	}
	return true
}

const setupInvalidCredentialMessage = "bootstrap credential is invalid or expired; " +
	"retrieve it again with `eshu admin initial-credential` or regenerate it with `eshu admin reset-initial-credential`"
