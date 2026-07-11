// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

var errLocalIdentityTOTPConfirmFixture = errors.New("totp confirm fixture error")

func TestHandleBeginTOTPEnrollment_RequiresAuthenticatedSession(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{}
	handler := &LocalIdentityHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/mfa/totp/begin", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestHandleBeginTOTPEnrollment_ReturnsProvisioningURIAndSealsSecret(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{
		resolvedUserID:      "user_owner",
		resolvedUserIDFound: true,
	}
	audit := &fakeGovernanceAuditAppender{}
	handler := &LocalIdentityHandler{
		Store:     store,
		Audit:     audit,
		NewSecret: sequenceSecrets("factor-totp-1"),
		Now:       func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/mfa/totp/begin",
		bytes.NewBufferString(`{"account_label":"owner@example.test"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		TenantID:      "tenant_local",
		WorkspaceID:   "workspace_local",
		SubjectIDHash: "sha256:owner-subject",
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var response localIdentityTOTPBeginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if response.FactorID != "factor-totp-1" {
		t.Fatalf("factor_id = %q, want factor-totp-1", response.FactorID)
	}
	if !strings.HasPrefix(response.OTPAuthURI, "otpauth://totp/Eshu:owner@example.test?") {
		t.Fatalf("otpauth_uri = %q, want an Eshu-issued otpauth URI", response.OTPAuthURI)
	}
	if !strings.Contains(response.OTPAuthURI, "secret=") || response.Secret == "" {
		t.Fatalf("response missing secret material: %#v", response)
	}
	if response.Digits != 6 || response.PeriodSeconds != 30 {
		t.Fatalf("digits/period = %d/%d, want 6/30", response.Digits, response.PeriodSeconds)
	}
	if store.totpBegin.UserID != "user_owner" || store.totpBegin.FactorID != "factor-totp-1" {
		t.Fatalf("store begin call = %#v, want resolved user_id and generated factor_id", store.totpBegin)
	}
	if len(store.totpBegin.SecretPlaintext) == 0 {
		t.Fatalf("store begin call missing secret plaintext")
	}
	if len(audit.events) != 1 || audit.events[0].Type != governanceaudit.EventTypeMFALifecycle {
		t.Fatalf("audit events = %#v, want mfa lifecycle event", audit.events)
	}
}

func TestHandleBeginTOTPEnrollment_UnknownSubjectReturnsNotFound(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{resolvedUserIDFound: false}
	handler := &LocalIdentityHandler{Store: store, NewSecret: sequenceSecrets("factor-totp-1")}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/local/mfa/totp/begin", bytes.NewBufferString(`{}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		SubjectIDHash: "sha256:unknown-subject",
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestHandleConfirmTOTPEnrollment_ActivatesOnSuccess(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 5, 0, 0, time.UTC)
	store := &fakeLocalIdentityStore{
		resolvedUserID:      "user_owner",
		resolvedUserIDFound: true,
	}
	audit := &fakeGovernanceAuditAppender{}
	handler := &LocalIdentityHandler{
		Store: store,
		Audit: audit,
		Now:   func() time.Time { return now },
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/mfa/totp/confirm",
		bytes.NewBufferString(`{"factor_id":"factor-totp-1","code":"123456"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		SubjectIDHash: "sha256:owner-subject",
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if store.totpConfirm.UserID != "user_owner" || store.totpConfirm.FactorID != "factor-totp-1" || store.totpConfirm.Code != "123456" {
		t.Fatalf("store confirm call = %#v, want resolved user_id + request fields", store.totpConfirm)
	}
	if len(audit.events) != 1 || audit.events[0].Type != governanceaudit.EventTypeMFALifecycle {
		t.Fatalf("audit events = %#v, want mfa lifecycle event", audit.events)
	}
}

func TestHandleConfirmTOTPEnrollment_WrongCodeReturnsBadRequest(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{
		resolvedUserID:      "user_owner",
		resolvedUserIDFound: true,
		totpConfirmError:    errLocalIdentityTOTPConfirmFixture,
	}
	handler := &LocalIdentityHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/mfa/totp/confirm",
		bytes.NewBufferString(`{"factor_id":"factor-totp-1","code":"000000"}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeBrowserSession,
		SubjectIDHash: "sha256:owner-subject",
	}))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// TestOnlyTOTPBeginResponseCarriesSecretJSONField is a static negative-
// leakage regression (issue #4986, mirroring the #4971 E2E leakage-scan
// shape): greps every non-test .go file in this package for a `json:"secret"`
// or `json:"otpauth_uri"` struct tag and asserts the only file that defines
// one is local_identity_totp.go (localIdentityTOTPBeginResponse — the one
// response that returns the plaintext secret, exactly once, by design). A
// future handler that accidentally re-exposes the secret on a read/status
// surface fails this test immediately instead of shipping silently.
func TestOnlyTOTPBeginResponseCarriesSecretJSONField(t *testing.T) {
	t.Parallel()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob *.go: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no .go files found in go/internal/query — glob pattern is broken")
	}
	secretTag := regexp.MustCompile(`json:"(secret|otpauth_uri)"`)
	var offenders []string
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		if path == "local_identity_totp.go" {
			continue
		}
		data, err := os.ReadFile(path) // #nosec G304 -- path comes from filepath.Glob("*.go") in this package's own directory, not external input
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if secretTag.Match(data) {
			offenders = append(offenders, path)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("found secret/otpauth_uri JSON field(s) outside local_identity_totp.go: %v", offenders)
	}
}

func TestHandleConfirmTOTPEnrollment_RequiresAuthenticatedSession(t *testing.T) {
	t.Parallel()

	store := &fakeLocalIdentityStore{}
	handler := &LocalIdentityHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/auth/local/mfa/totp/confirm",
		bytes.NewBufferString(`{"factor_id":"factor-totp-1","code":"123456"}`),
	)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}
