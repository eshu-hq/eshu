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
// credential, generate a fresh set of MFA recovery codes, issue the browser
// session, and only THEN atomically rotate MFA and permanently consume the
// bootstrap credential (sealing every setup route shut per #4965). The
// generated codes are returned in the response body exactly once — the
// console must render them with copy-all/download and gate navigation on
// the operator confirming they saved them — and are never logged or
// persisted in clear text; only their hashes reach storage
// (localIdentityHashes).
//
// Session issuance runs BEFORE the irreversible seal (#4990 P2): if it
// fails, nothing has been persisted yet (MFA is untouched, the bootstrap
// credential is still unconsumed, SetupNeeded still reports true), so the
// operator can simply retry the call — no recovery codes are ever
// generated, hashed, and then discarded behind a failed response the
// operator never sees.
//
// The MFA rotation + credential consumption itself runs through
// SetupStore.CompleteSetupMFA, one atomic transaction / advisory-locked
// critical section (#4990 P1): two concurrent /setup/mfa calls for the same
// owner can both reach this point (both already reproved the credential and
// both already have a session), but only one of them can win the race
// inside that critical section. The loser's generated codes were never
// persisted — this handler fails closed with 409 rather than claim success
// for codes nobody can use.
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
		// issueLocalSessionCookies already wrote the error response. Nothing
		// was persisted yet — the wizard stays reachable and the operator
		// can retry the whole call.
		h.recordOutcome(r, "mfa_error")
		return
	}

	mfaFactorID, err := h.newID()
	if err != nil {
		h.recordOutcome(r, "mfa_error")
		WriteError(w, http.StatusInternalServerError, "failed to complete setup")
		return
	}

	completed, err := h.Store.CompleteSetupMFA(r.Context(), CompleteSetupMFAInput{
		TenantID:           owner.TenantID,
		WorkspaceID:        owner.WorkspaceID,
		SubjectIDHash:      owner.SubjectIDHash,
		UserID:             owner.UserID,
		MFAFactorID:        mfaFactorID,
		MFAFactorKind:      "recovery_code",
		RecoveryCodeHashes: localIdentityHashes(codes),
		Now:                now,
	})
	if err != nil {
		h.recordOutcome(r, "mfa_error")
		WriteError(w, http.StatusInternalServerError, "failed to complete setup")
		return
	}
	if !completed {
		// A concurrent /setup/mfa call already won the advisory-locked race
		// (#4990 P1). This caller's generated recovery codes were never
		// persisted — fail closed rather than claim success for codes
		// nobody can use. The session issued above is still valid (the
		// operator legitimately reproved ownership), so this is a clean
		// rejection, not a lockout: the operator can sign in with the
		// credentials the WINNING request set.
		h.auditSetup(r, governanceaudit.DecisionDenied, "setup_mfa_concurrent_completion")
		h.recordOutcome(r, "mfa_denied")
		WriteError(w, http.StatusConflict, "setup was already completed by a concurrent request")
		return
	}

	h.auditSetup(r, governanceaudit.DecisionAllowed, "setup_completed")
	h.recordOutcome(r, "mfa_allowed")
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
