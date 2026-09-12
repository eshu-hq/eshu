// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package local

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
)

// This file proves the two self-service TOTP enrollment routes end to end
// through Mount + ServeHTTP (#6642 route-coverage; see handler_test.go's
// file comment for why these tests are new rather than moved verbatim).

func ownerSubjectContext(r *http.Request) *http.Request {
	auth := queryauth.AuthContext{Mode: queryauth.AuthModeBrowserSession, SubjectIDHash: "sha256:owner"}
	return r.WithContext(queryauth.ContextWithAuthContext(r.Context(), auth))
}

func TestBeginTOTPEnrollment(t *testing.T) {
	t.Parallel()

	store := &fakeStore{resolvedUserID: "user-owner", resolvedUserIDFound: true}
	handler := &IdentityHandler{Store: store, NewSecret: sequenceSecrets("factor-totp-1")}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/mfa/totp/begin", bytes.NewBufferString(
		`{"account_label":"owner@example.test"}`,
	))
	req = ownerSubjectContext(req)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if store.totpBegin.UserID != "user-owner" || store.totpBegin.FactorID != "factor-totp-1" {
		t.Fatalf("totp begin = %#v, want resolved user and generated factor", store.totpBegin)
	}
}

func TestBeginTOTPEnrollmentRequiresAuthenticatedSession(t *testing.T) {
	t.Parallel()

	handler := &IdentityHandler{Store: &fakeStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/mfa/totp/begin", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestConfirmTOTPEnrollment(t *testing.T) {
	t.Parallel()

	store := &fakeStore{resolvedUserID: "user-owner", resolvedUserIDFound: true}
	handler := &IdentityHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/mfa/totp/confirm", bytes.NewBufferString(
		`{"factor_id":"factor-totp-1","code":"123456"}`,
	))
	req = ownerSubjectContext(req)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if store.totpConfirm.UserID != "user-owner" || store.totpConfirm.Code != "123456" {
		t.Fatalf("totp confirm = %#v, want resolved user and submitted code", store.totpConfirm)
	}
}

func TestConfirmTOTPEnrollmentWrongCodeReturnsBadRequest(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		resolvedUserID:      "user-owner",
		resolvedUserIDFound: true,
		totpConfirmError:    errTOTPConfirmFixture,
	}
	handler := &IdentityHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/mfa/totp/confirm", bytes.NewBufferString(
		`{"factor_id":"factor-totp-1","code":"000000"}`,
	))
	req = ownerSubjectContext(req)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
