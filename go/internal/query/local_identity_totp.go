// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/base32"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/totp"
)

// localIdentityTOTPIssuer is the otpauth:// issuer label every enrollment
// URI carries (issue #4986). It identifies the service in the
// authenticator app's entry list; it is not a secret.
const localIdentityTOTPIssuer = "Eshu"

// localIdentityTOTPBeginRequest carries an optional client-supplied account
// label for the authenticator app entry. The server never has the caller's
// original login identifier (sessions carry only subject_id_hash, a
// one-way hash — see AuthContext), so the console supplies a human label it
// already knows from its own session state; a missing label falls back to
// a generic default. This label is cosmetic only, never used for lookup or
// authorization.
type localIdentityTOTPBeginRequest struct {
	AccountLabel string `json:"account_label"`
}

// localIdentityTOTPBeginResponse is returned once per BeginLocalIdentityTOTPEnrollment
// call. Secret and OTPAuthURI both carry the plaintext shared secret — this
// is the ONE response that ever does; every other MFA/profile read in this
// package omits it. The console must render the QR/manual-entry view
// immediately and never re-request it (a re-fetch is not possible: the
// secret is sealed at rest immediately after this response is built and
// never read back out except by ConfirmLocalIdentityTOTPEnrollment's
// server-side verification).
type localIdentityTOTPBeginResponse struct {
	FactorID      string `json:"factor_id"`
	OTPAuthURI    string `json:"otpauth_uri"`
	Secret        string `json:"secret"`
	Issuer        string `json:"issuer"`
	Digits        int    `json:"digits"`
	PeriodSeconds int    `json:"period_seconds"`
}

type localIdentityTOTPConfirmRequest struct {
	FactorID string `json:"factor_id"`
	Code     string `json:"code"`
}

// mountTOTPRoutes registers the self-service TOTP enrollment routes (issue
// #4986). Both routes require an existing authenticated session (any local
// user may enroll their own second factor) and always scope to the calling
// session's own subject — neither ever accepts a target user id from the
// request body.
func (h *LocalIdentityHandler) mountTOTPRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/auth/local/mfa/totp/begin", h.handleBeginTOTPEnrollment)
	mux.HandleFunc("POST /api/v0/auth/local/mfa/totp/confirm", h.handleConfirmTOTPEnrollment)
}

func (h *LocalIdentityHandler) handleBeginTOTPEnrollment(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	auth, ok := AuthContextFromContext(r.Context())
	auth = normalizeAuthContext(auth)
	if !ok || auth.SubjectIDHash == "" {
		unauthorizedResponse(w, r)
		return
	}
	var req localIdentityTOTPBeginRequest
	if err := readOptionalTOTPBeginRequest(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid totp enrollment request")
		return
	}
	userID, found, err := h.Store.ResolveLocalIdentityUserID(r.Context(), auth.SubjectIDHash)
	if err != nil {
		slog.ErrorContext(r.Context(), "resolve local identity user id failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to begin totp enrollment")
		return
	}
	if !found {
		WriteError(w, http.StatusNotFound, "local identity not found")
		return
	}
	secret, err := totp.GenerateSecret()
	if err != nil {
		slog.ErrorContext(r.Context(), "generate totp secret failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to begin totp enrollment")
		return
	}
	factorID := h.newID()
	if factorID == "" {
		WriteError(w, http.StatusInternalServerError, "failed to begin totp enrollment")
		return
	}
	uri, err := totp.ProvisioningURI(totp.ProvisioningURIParams{
		Issuer:  localIdentityTOTPIssuer,
		Account: localIdentityDefault(req.AccountLabel, "account"),
		Secret:  secret,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "build totp provisioning uri failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to begin totp enrollment")
		return
	}
	if err := h.Store.BeginLocalIdentityTOTPEnrollment(r.Context(), LocalIdentityTOTPEnrollmentBegin{
		UserID:          userID,
		FactorID:        factorID,
		SecretPlaintext: secret,
		CreatedAt:       h.now(),
	}); err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeMFALifecycle, governanceaudit.DecisionDenied, "totp_enrollment_begin_failed", auth.SubjectIDHash)
		WriteError(w, http.StatusBadRequest, "failed to begin totp enrollment")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeMFALifecycle, governanceaudit.DecisionAllowed, "totp_enrollment_begin", auth.SubjectIDHash)
	WriteJSON(w, http.StatusCreated, localIdentityTOTPBeginResponse{
		FactorID:      factorID,
		OTPAuthURI:    uri,
		Secret:        base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret),
		Issuer:        localIdentityTOTPIssuer,
		Digits:        totp.DefaultDigits,
		PeriodSeconds: int(totp.DefaultStep.Seconds()),
	})
}

func (h *LocalIdentityHandler) handleConfirmTOTPEnrollment(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	auth, ok := AuthContextFromContext(r.Context())
	auth = normalizeAuthContext(auth)
	if !ok || auth.SubjectIDHash == "" {
		unauthorizedResponse(w, r)
		return
	}
	var req localIdentityTOTPConfirmRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid totp confirm request")
		return
	}
	userID, found, err := h.Store.ResolveLocalIdentityUserID(r.Context(), auth.SubjectIDHash)
	if err != nil {
		slog.ErrorContext(r.Context(), "resolve local identity user id failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to confirm totp enrollment")
		return
	}
	if !found {
		WriteError(w, http.StatusNotFound, "local identity not found")
		return
	}
	if err := h.Store.ConfirmLocalIdentityTOTPEnrollment(r.Context(), LocalIdentityTOTPEnrollmentConfirm{
		UserID:   userID,
		FactorID: strings.TrimSpace(req.FactorID),
		Code:     req.Code,
		Now:      h.now(),
	}); err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeMFALifecycle, governanceaudit.DecisionDenied, "totp_enrollment_confirm_failed", auth.SubjectIDHash)
		WriteError(w, http.StatusBadRequest, "failed to confirm totp enrollment")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeMFALifecycle, governanceaudit.DecisionAllowed, "totp_enrollment_confirmed", auth.SubjectIDHash)
	w.WriteHeader(http.StatusNoContent)
}

// readOptionalTOTPBeginRequest mirrors readOptionalAPITokenRevokeRequest
// (local_identity_api_tokens.go): an empty body is valid (account_label is
// optional), but malformed JSON is still rejected.
func readOptionalTOTPBeginRequest(r *http.Request, req *localIdentityTOTPBeginRequest) error {
	if r == nil || r.Body == nil {
		return nil
	}
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(req); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}
