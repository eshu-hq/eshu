// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// The shared test double rides the pinned admission shape, not a parallel
// signature: if the port re-signs any of its five methods, this file fails
// compilation the same way the frozen contract test fails. It proves every
// wire proof below exercises the pinned setup writer, not a lookalike.
var _ SetupAdmission = (*writerPinSetupStore)(nil)

// writerPinSetupStore delegates to the shared setup double and records the
// seal call in the shared order log, so the session-before-seal test can
// prove the irreversible step runs only after session issuance.
type writerPinSetupStore struct {
	*fakeSetupStore
	order *[]string
}

func (s *writerPinSetupStore) CompleteSetupMFA(ctx context.Context, in CompleteSetupMFAInput) (bool, error) {
	if s.order != nil {
		*s.order = append(*s.order, "seal")
	}
	return s.fakeSetupStore.CompleteSetupMFA(ctx, in)
}

// writerPinSessionStore is a failing-capable BrowserSessionStore double: it
// records issuance in the shared order log next to the seal above, proving
// the wizard issues the session before it seals.
type writerPinSessionStore struct {
	createErr error
	order     *[]string
	created   int
}

func (s *writerPinSessionStore) CreateBrowserSession(_ context.Context, _ BrowserSessionCreateRecord) error {
	if s.order != nil {
		*s.order = append(*s.order, "session")
	}
	s.created++
	return s.createErr
}

func (s *writerPinSessionStore) RevokeBrowserSession(_ context.Context, _ string, _ time.Time) error {
	return nil
}

func (s *writerPinSessionStore) SwitchBrowserSessionWorkspace(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ time.Time,
) (AuthContext, bool, error) {
	return AuthContext{}, false, nil
}

func openWriterPinWizard(order *[]string) *writerPinSetupStore {
	return &writerPinSetupStore{
		fakeSetupStore: &fakeSetupStore{
			needsSetup:    true,
			validUsername: "admin",
			validPassword: "operator-chosen-password",
			owner: SetupOwner{
				UserID:        "user-1",
				SubjectIDHash: "sha256:owner-subject",
				TenantID:      "default",
				WorkspaceID:   "default",
			},
			completeMFAResult: true,
		},
		order: order,
	}
}

func serveWriterPinSetup(
	t *testing.T,
	store SetupStore,
	sessions *writerPinSessionStore,
	audit *fakeGovernanceAuditAppender,
	method, path, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	handler := &SetupHandler{
		Store:     store,
		Sessions:  sessions,
		Audit:     audit,
		NewSecret: sequenceSecrets("session-secret", "csrf-secret", "mfa-factor-id"),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestSetupWriterWireSealedWizardAnswers410 proves every mutating setup leg
// behind SetupAdmission v0 fails closed with 410 once SetupNeeded reports
// false, while the read-only state check still answers normally — and that
// the 410 path emits no audit event (logged-vs-discarded asymmetry: a sealed
// wizard is steady state, not an operator action).
func TestSetupWriterWireSealedWizardAnswers410(t *testing.T) {
	t.Parallel()

	store := &fakeSetupStore{needsSetup: false}
	audit := &fakeGovernanceAuditAppender{}
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v0/auth/setup/claim", `{"username":"admin","password":"x"}`},
		{http.MethodPost, "/api/v0/auth/setup/admin", `{"username":"admin","password":"x","new_password":"long-enough-password"}`},
		{http.MethodPost, "/api/v0/auth/setup/mfa", `{"username":"admin","password":"x"}`},
	} {
		rec := serveWriterPinSetup(t, store, &writerPinSessionStore{}, audit, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusGone {
			t.Fatalf("%s %s status = %d, want %d: %s", tc.method, tc.path, rec.Code, http.StatusGone, rec.Body.String())
		}
	}
	if len(audit.events) != 0 {
		t.Fatalf("audit events = %#v, want none: the sealed 410 path is discarded, not logged", audit.events)
	}

	rec := serveWriterPinSetup(t, store, &writerPinSessionStore{}, audit,
		http.MethodGet, "/api/v0/auth/setup-state", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("setup-state status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestSetupWriterWireWrongCredentialIsIndistinguishable401 proves the claim,
// admin, and MFA legs behind the pinned writer answer the identical 401 for
// every wrong guess — the same body on all three legs — so a wrong guess
// cannot probe whether the bootstrap credential still exists. Each leg logs
// its own denied reason.
func TestSetupWriterWireWrongCredentialIsIndistinguishable401(t *testing.T) {
	t.Parallel()

	legs := []struct {
		path   string
		body   string
		reason string
	}{
		{"/api/v0/auth/setup/claim", `{"username":"admin","password":"wrong"}`, "setup_claim_invalid"},
		{"/api/v0/auth/setup/admin", `{"username":"admin","password":"wrong","new_password":"long-enough-password"}`, "setup_admin_invalid"},
		{"/api/v0/auth/setup/mfa", `{"username":"admin","password":"wrong"}`, "setup_mfa_invalid"},
	}
	bodies := make([]string, 0, len(legs))
	for _, leg := range legs {
		leg := leg
		store := openWriterPinWizard(nil)
		audit := &fakeGovernanceAuditAppender{}
		rec := serveWriterPinSetup(t, store, &writerPinSessionStore{}, audit,
			http.MethodPost, leg.path, leg.body)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d: %s", leg.path, rec.Code, http.StatusUnauthorized, rec.Body.String())
		}
		bodies = append(bodies, rec.Body.String())
		if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionDenied ||
			audit.events[0].ReasonCode != leg.reason {
			t.Fatalf("%s audit events = %#v, want exactly one denied %q event", leg.path, audit.events, leg.reason)
		}
		if store.completeMFACalls != 0 {
			t.Fatalf("%s sealed the wizard on a rejected credential", leg.path)
		}
	}
	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Fatalf("401 bodies differ across legs, leaking which step failed: %q vs %q", bodies[0], bodies[i])
		}
	}
}

// TestSetupWriterWireConcurrentLoserAnswers409 proves the exactly-once leg
// the pin record names: when the store reports completed=false (a concurrent
// /setup/mfa call already won the seal), the handler fails closed with 409
// and never claims completion for codes nobody can use. The operator already
// reproved ownership, so the issued session stays valid — but the response
// body carries no completion status and no recovery codes.
func TestSetupWriterWireConcurrentLoserAnswers409(t *testing.T) {
	t.Parallel()

	store := openWriterPinWizard(nil)
	store.completeMFAResult = false // a concurrent caller already won.
	sessions := &writerPinSessionStore{}
	audit := &fakeGovernanceAuditAppender{}
	rec := serveWriterPinSetup(t, store, sessions, audit,
		http.MethodPost, "/api/v0/auth/setup/mfa", `{"username":"admin","password":"operator-chosen-password"}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"status":"completed"`) {
		t.Fatalf("response falsely claimed completion for a losing race: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "recovery_codes") {
		t.Fatalf("response carried recovery codes for a losing race: %s", rec.Body.String())
	}
	if sessions.created != 1 {
		t.Fatalf("created sessions = %d, want 1: the loser reproved ownership, only the seal lost", sessions.created)
	}
	found := false
	for _, event := range audit.events {
		if event.Decision == governanceaudit.DecisionDenied && event.ReasonCode == "setup_mfa_concurrent_completion" {
			found = true
		}
	}
	if !found {
		t.Fatalf("audit events = %#v, want a setup_mfa_concurrent_completion denial", audit.events)
	}
}

// TestSetupWriterWireWinnerEmitsRecoveryCodesExactlyOnce proves the winning
// seal returns the generated recovery codes in the response body exactly
// once while persisting only hashes: the store receives exactly
// setupRecoveryCodeCount sha256: hashes, none of which contains a plaintext
// code, and every returned code is unique.
func TestSetupWriterWireWinnerEmitsRecoveryCodesExactlyOnce(t *testing.T) {
	t.Parallel()

	store := openWriterPinWizard(nil)
	sessions := &writerPinSessionStore{}
	audit := &fakeGovernanceAuditAppender{}
	rec := serveWriterPinSetup(t, store, sessions, audit,
		http.MethodPost, "/api/v0/auth/setup/mfa", `{"username":"admin","password":"operator-chosen-password"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body SetupCompleteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if body.Status != "completed" {
		t.Fatalf("response status = %q, want completed", body.Status)
	}
	if len(body.RecoveryCodes) != setupRecoveryCodeCount || setupRecoveryCodeCount == 0 {
		t.Fatalf("recovery codes = %d, want %d", len(body.RecoveryCodes), setupRecoveryCodeCount)
	}
	seen := make(map[string]struct{}, len(body.RecoveryCodes))
	for _, code := range body.RecoveryCodes {
		if _, dup := seen[code]; dup {
			t.Fatalf("duplicate recovery code %q: codes must be unique", code)
		}
		seen[code] = struct{}{}
	}
	if len(store.completeMFAInput.RecoveryCodeHashes) != len(body.RecoveryCodes) {
		t.Fatalf("stored hashes = %d, want one per returned code (%d)",
			len(store.completeMFAInput.RecoveryCodeHashes), len(body.RecoveryCodes))
	}
	for _, hash := range store.completeMFAInput.RecoveryCodeHashes {
		if !strings.HasPrefix(hash, "sha256:") {
			t.Fatalf("stored recovery hash %q is not hash-only", hash)
		}
		for _, code := range body.RecoveryCodes {
			if strings.Contains(hash, code) {
				t.Fatalf("stored hash embeds plaintext code: codes must persist hash-only")
			}
		}
	}
	if store.completeMFAInput.MFAFactorID == "" {
		t.Fatal("seal ran without an MFA factor id")
	}
	if sessions.created != 1 {
		t.Fatalf("created sessions = %d, want 1 (wizard completion logs the operator in)", sessions.created)
	}
	found := false
	for _, event := range audit.events {
		if event.Decision == governanceaudit.DecisionAllowed && event.ReasonCode == "setup_completed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("audit events = %#v, want an allowed setup_completed event", audit.events)
	}
}

// TestSetupWriterWireSessionFailureNeverSeals proves the session-before-seal
// ordering the pin record names: when session issuance fails, nothing has
// been persisted yet — the seal never runs, so the wizard stays reachable
// and the operator can retry the whole call.
func TestSetupWriterWireSessionFailureNeverSeals(t *testing.T) {
	t.Parallel()

	store := openWriterPinWizard(nil)
	sessions := &writerPinSessionStore{createErr: errors.New("session store unavailable")}
	audit := &fakeGovernanceAuditAppender{}
	rec := serveWriterPinSetup(t, store, sessions, audit,
		http.MethodPost, "/api/v0/auth/setup/mfa", `{"username":"admin","password":"operator-chosen-password"}`)

	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want a failure: the seal must not succeed without a session", rec.Code)
	}
	if store.completeMFACalls != 0 {
		t.Fatal("CompleteSetupMFA ran after session issuance failed: the seal must wait for the session")
	}
}

// TestSetupWriterWireSessionIssuedBeforeSeal proves the ordering directly:
// on the winning path the fake session store records issuance before the
// admission port records the seal, so the seal can never orphan recovery
// codes behind a failed response the operator never sees.
func TestSetupWriterWireSessionIssuedBeforeSeal(t *testing.T) {
	t.Parallel()

	var order []string
	store := openWriterPinWizard(&order)
	sessions := &writerPinSessionStore{order: &order}
	rec := serveWriterPinSetup(t, store, sessions, &fakeGovernanceAuditAppender{},
		http.MethodPost, "/api/v0/auth/setup/mfa", `{"username":"admin","password":"operator-chosen-password"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(order) != 2 || order[0] != "session" || order[1] != "seal" {
		t.Fatalf("call order = %q, want [session seal]: issuance must precede the seal", order)
	}
}

// TestSetupWriterWireAuditReasonCodes proves the audit matrix the pin record
// names: every successful leg logs its allowed reason, every credential
// rejection logs its denied reason, and shape-only rejections plus the
// sealed 410 path emit nothing (logged-vs-discarded asymmetry).
func TestSetupWriterWireAuditReasonCodes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
		reason string
		allow  bool
	}{
		{"claim allowed", http.MethodPost, "/api/v0/auth/setup/claim", `{"username":"admin","password":"operator-chosen-password"}`, "setup_claimed", true},
		{"admin allowed", http.MethodPost, "/api/v0/auth/setup/admin", `{"username":"admin","password":"operator-chosen-password","new_password":"long-enough-password"}`, "setup_admin_password_set", true},
		{"mfa allowed", http.MethodPost, "/api/v0/auth/setup/mfa", `{"username":"admin","password":"operator-chosen-password"}`, "setup_completed", true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := openWriterPinWizard(nil)
			audit := &fakeGovernanceAuditAppender{}
			rec := serveWriterPinSetup(t, store, &writerPinSessionStore{}, audit, tc.method, tc.path, tc.body)

			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s status = %d, want %d: %s", tc.method, tc.path, rec.Code, http.StatusOK, rec.Body.String())
			}
			if len(audit.events) != 1 {
				t.Fatalf("%s audit events = %#v, want exactly one event", tc.path, audit.events)
			}
			want := governanceaudit.DecisionDenied
			if tc.allow {
				want = governanceaudit.DecisionAllowed
			}
			if audit.events[0].Decision != want || audit.events[0].ReasonCode != tc.reason {
				t.Fatalf("%s audit event = %+v, want decision %q reason %q", tc.path, audit.events[0], want, tc.reason)
			}
		})
	}

	// Discarded: a shape-only password rejection never even reproves the
	// credential, and a sealed wizard emits no 410 event.
	store := openWriterPinWizard(nil)
	audit := &fakeGovernanceAuditAppender{}
	rec := serveWriterPinSetup(t, store, &writerPinSessionStore{}, audit,
		http.MethodPost, "/api/v0/auth/setup/admin", `{"username":"admin","password":"operator-chosen-password","new_password":"short"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("short-password status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(audit.events) != 0 {
		t.Fatalf("short-password audit events = %#v, want none: shape rejections are discarded", audit.events)
	}

	sealed := &fakeSetupStore{needsSetup: false}
	sealedAudit := &fakeGovernanceAuditAppender{}
	rec = serveWriterPinSetup(t, sealed, &writerPinSessionStore{}, sealedAudit,
		http.MethodPost, "/api/v0/auth/setup/claim", `{"username":"admin","password":"x"}`)
	if rec.Code != http.StatusGone {
		t.Fatalf("sealed claim status = %d, want %d", rec.Code, http.StatusGone)
	}
	if len(sealedAudit.events) != 0 {
		t.Fatalf("sealed audit events = %#v, want none", sealedAudit.events)
	}
}
