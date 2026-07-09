// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// setupRecoveryCodeCount is the number of one-time MFA recovery codes the
// final wizard step generates. Matches the count local identity invitations
// commonly seed; enough for repeated recovery without being unwieldy to copy.
const setupRecoveryCodeCount = 10

// setupRecoveryCodeBytes is the raw entropy per generated recovery code.
const setupRecoveryCodeBytes = 20

// handleCompleteMFA is the wizard's final step: reprove the bootstrap
// credential, generate and persist a fresh set of MFA recovery codes,
// permanently consume the bootstrap credential (sealing every setup route
// shut per #4965), and issue a browser session so the operator lands
// logged-in with no separate login step. The generated codes are returned
// in the response body exactly once — the console must render them with
// copy-all/download and gate navigation on the operator confirming they
// saved them — and are never logged or persisted in clear text; only their
// hashes reach storage (localIdentityHashes).
func (h *SetupHandler) handleCompleteMFA(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireSetupOpen(w, r) {
		return
	}
	var req setupMFARequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid setup mfa request")
		return
	}
	owner, ok := h.verifyAndResolveOwner(w, r, req.Username, req.Password, "mfa")
	if !ok {
		return
	}
	codes, err := h.generateRecoveryCodes()
	if err != nil {
		h.recordOutcome(r, "mfa_error")
		WriteError(w, http.StatusInternalServerError, "failed to generate recovery codes")
		return
	}
	now := h.now()
	if err := h.Store.RotateSetupMFA(r.Context(), LocalIdentityMFAReset{
		UserID:             owner.UserID,
		MFAFactorID:        h.newID(),
		MFAFactorKind:      "recovery_code",
		RecoveryCodeHashes: localIdentityHashes(codes),
		ResetAt:            now,
	}); err != nil {
		h.recordOutcome(r, "mfa_error")
		WriteError(w, http.StatusInternalServerError, "failed to enroll recovery codes")
		return
	}
	if err := h.Store.CompleteSetup(r.Context(), owner.SubjectIDHash, now); err != nil {
		h.recordOutcome(r, "mfa_error")
		WriteError(w, http.StatusInternalServerError, "failed to complete setup")
		return
	}
	h.auditSetup(r, governanceaudit.DecisionAllowed, "setup_completed")
	h.recordOutcome(r, "mfa_allowed")

	issued, sessionOK := issueLocalSessionCookies(
		w, r, h.Sessions, h.newSecret, now, h.idleTimeout(), h.absoluteTimeout(), h.cookieSecureMode(),
		LocalIdentityAuthContext{
			TenantID:      owner.TenantID,
			WorkspaceID:   owner.WorkspaceID,
			SubjectIDHash: owner.SubjectIDHash,
			SubjectClass:  "local_user",
			AllScopes:     true,
		},
	)
	if !sessionOK {
		// Setup itself is already complete and sealed by this point — only
		// session issuance failed. issueLocalSessionCookies already wrote the
		// error response; the operator can sign in normally with the
		// password/recovery codes they just set.
		return
	}
	WriteJSON(w, http.StatusOK, SetupCompleteResponse{
		Status:            "completed",
		RecoveryCodes:     codes,
		Auth:              issued.Auth,
		CSRFToken:         issued.CSRFToken,
		IdleExpiresAt:     issued.IdleExpiresAt,
		AbsoluteExpiresAt: issued.AbsoluteExpiresAt,
	})
}

// generateRecoveryCodes returns setupRecoveryCodeCount fresh crypto/rand
// recovery codes. It does not use h.newSecret (which tests may override to a
// deterministic sequence for session-secret assertions) so recovery-code
// generation stays unconditionally random even under test doubles.
func (h *SetupHandler) generateRecoveryCodes() ([]string, error) {
	codes := make([]string, 0, setupRecoveryCodeCount)
	for i := 0; i < setupRecoveryCodeCount; i++ {
		var buf [setupRecoveryCodeBytes]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return nil, err
		}
		codes = append(codes, base64.RawURLEncoding.EncodeToString(buf[:]))
	}
	return codes, nil
}
