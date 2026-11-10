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
// signature: if the port re-signs UpsertSignInPolicy, this file fails
// compilation the same way the frozen contract test fails. It proves every
// wire proof below exercises the pinned writer, not a lookalike.
var _ SignInPolicyAdmission = (*writerPinSignInPolicyStore)(nil)

type writerPinSignInPolicyStore struct {
	result   SignInPolicy
	err      error
	calls    int
	tenantID string
}

func (s *writerPinSignInPolicyStore) UpsertSignInPolicy(
	_ context.Context,
	tenantID string,
	_ SignInPolicyUpdateRequest,
	_ string,
	_ time.Time,
) (SignInPolicy, error) {
	s.calls++
	s.tenantID = tenantID
	if s.err != nil {
		return SignInPolicy{}, s.err
	}
	return s.result, nil
}

// store takes the admission interface, not the concrete double, so the
// nil-store test passes an untyped nil exactly like the production wiring
// does after unwrapNilAdmissionPort — a typed-nil concrete pointer would
// hide from the handler's nil-store check.
func serveWriterPinPatch(
	t *testing.T,
	store SignInPolicyAdmission,
	audit *fakeSignInPolicyAudit,
	req *http.Request,
) *httptest.ResponseRecorder {
	t.Helper()
	handler := &SignInPolicyMutationHandler{Store: store, Audit: audit}
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// encodeWriterPinJSON encodes through the same envelope-free JSON writer the
// handler uses (json.Encoder, HTML unescaped, trailing newline), so the byte
// comparison below pins the served bytes, not a re-serialization.
func encodeWriterPinJSON(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("encode expected projection: %v", err)
	}
	return buf.Bytes()
}

// TestSignInPolicyWriterWireSuccessIsByteExactProjection proves the PATCH leg
// behind SignInPolicyAdmission v0 serves exactly the core detail projection
// of the SignInPolicy value the pinned writer returns: the served bytes are
// byte-identical to the shared signInPolicyDetailJSON projection, so a dest
// consumer forwarding core bytes serves this leg verbatim by construction.
func TestSignInPolicyWriterWireSuccessIsByteExactProjection(t *testing.T) {
	t.Parallel()

	policy := SignInPolicy{
		TenantID:                         "tenant_a",
		RequireSSO:                       true,
		AllowLocalUserCreation:           false,
		RequireMFAForAllUsers:            true,
		IdleTimeoutSeconds:               900,
		AbsoluteTimeoutSeconds:           43200,
		SSOAdminVerifiedAt:               time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		SSOAdminVerifiedProviderConfigID: "pc_abc",
		PolicyRevisionHash:               "sha256:rev1",
		UpdatedAt:                        time.Date(2026, 7, 2, 3, 4, 5, 0, time.UTC),
	}
	store := &writerPinSignInPolicyStore{result: policy}
	audit := &fakeSignInPolicyAudit{}
	rec := serveWriterPinPatch(t, store, audit, adminSignInPolicyRequest(`{"require_sso":true}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	want := encodeWriterPinJSON(t, signInPolicyDetailJSON(policy))
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatalf("PATCH body = %s, want byte-exact projection %s", rec.Body.String(), string(want))
	}
	if store.tenantID != "tenant_a" {
		t.Fatalf("store called with tenant_id = %q, want tenant_a", store.tenantID)
	}
	if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionAllowed ||
		audit.events[0].ReasonCode != "sign_in_policy_updated" {
		t.Fatalf("audit events = %#v, want exactly one allowed sign_in_policy_updated event", audit.events)
	}
}

// TestSignInPolicyWriterWireSentinelsMapTo400 proves every store-side
// rejection behind the pinned writer — both require_sso guardrails and the
// merged timeout-ordering check the store enforces under its row lock — maps
// to 400 with its own denied audit reason, never a 500.
func TestSignInPolicyWriterWireSentinelsMapTo400(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		err    error
		reason string
	}{
		{"no proven provider", ErrSignInPolicyGuardrailNoProvenProvider, "sign_in_policy_guardrail_no_provider"},
		{"no SSO admin proof", ErrSignInPolicyGuardrailNoSSOAdminProof, "sign_in_policy_guardrail_no_sso_proof"},
		{"timeout ordering", ErrSignInPolicyTimeoutOrdering, "sign_in_policy_invalid_timeout"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &writerPinSignInPolicyStore{err: tc.err}
			audit := &fakeSignInPolicyAudit{}
			rec := serveWriterPinPatch(t, store, audit, adminSignInPolicyRequest(`{"require_sso":true}`))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionDenied ||
				audit.events[0].ReasonCode != tc.reason {
				t.Fatalf("audit events = %#v, want exactly one denied %q event", audit.events, tc.reason)
			}
		})
	}
}

// TestSignInPolicyWriterWireInvalidBodyMapsTo400 proves handler-side shape
// rejections fail before the pinned writer runs: malformed JSON and both
// timeout-shape violations answer 400 with a denied audit event, and the
// store sees zero calls.
func TestSignInPolicyWriterWireInvalidBodyMapsTo400(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		body   string
		reason string
	}{
		{"malformed JSON", `{"require_sso":`, "sign_in_policy_invalid_request"},
		{"negative idle", `{"idle_timeout_seconds":-1}`, "sign_in_policy_invalid_timeout"},
		{"absolute below idle", `{"idle_timeout_seconds":3600,"absolute_timeout_seconds":1800}`, "sign_in_policy_invalid_timeout"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &writerPinSignInPolicyStore{}
			audit := &fakeSignInPolicyAudit{}
			rec := serveWriterPinPatch(t, store, audit, adminSignInPolicyRequest(tc.body))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if store.calls != 0 {
				t.Fatalf("UpsertSignInPolicy calls = %d, want 0 for a rejected body", store.calls)
			}
			if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionDenied ||
				audit.events[0].ReasonCode != tc.reason {
				t.Fatalf("audit events = %#v, want exactly one denied %q event", audit.events, tc.reason)
			}
		})
	}
}

// TestSignInPolicyWriterWirePermissionDeniedMapsTo403 proves the catalog
// gate runs before the pinned writer: an enforced caller without the
// identity_admin feature answers 403 with the capability-carrying envelope
// and a permission_catalog_denied audit event, and the store stays silent.
func TestSignInPolicyWriterWirePermissionDeniedMapsTo403(t *testing.T) {
	t.Parallel()

	store := &writerPinSignInPolicyStore{}
	audit := &fakeSignInPolicyAudit{}
	req := httptest.NewRequest(http.MethodPatch, "/api/v0/auth/admin/sign-in-policy", bytes.NewBufferString(`{}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), catalogEnforcedAdminAuth("tokens")))
	rec := serveWriterPinPatch(t, store, audit, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "identity_admin.sign_in_policy_update") {
		t.Fatalf("body = %s, want the denied capability named", rec.Body.String())
	}
	if store.calls != 0 {
		t.Fatalf("UpsertSignInPolicy calls = %d, want 0 for a permission-denied caller", store.calls)
	}
	if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionDenied ||
		audit.events[0].ReasonCode != "permission_catalog_denied" {
		t.Fatalf("audit events = %#v, want exactly one denied permission_catalog_denied event", audit.events)
	}
}

// TestSignInPolicyWriterWireScopeDeniedMapsTo403 proves the all-scope admin
// check runs before the pinned writer: a workspace-scoped caller and a
// tenantless caller each answer 403 with their own denied audit reason, and
// the store stays silent.
func TestSignInPolicyWriterWireScopeDeniedMapsTo403(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		ctx    AuthContext
		reason string
	}{
		{"workspace scoped", AuthContext{Mode: AuthModeBrowserSession, AllScopes: false, TenantID: "tenant_a"}, "admin_scope_required"},
		{"tenantless", AuthContext{Mode: AuthModeBrowserSession, AllScopes: true}, "admin_tenant_required"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &writerPinSignInPolicyStore{}
			audit := &fakeSignInPolicyAudit{}
			req := httptest.NewRequest(http.MethodPatch, "/api/v0/auth/admin/sign-in-policy", bytes.NewBufferString(`{}`))
			req = req.WithContext(ContextWithAuthContext(req.Context(), tc.ctx))
			rec := serveWriterPinPatch(t, store, audit, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
			}
			if store.calls != 0 {
				t.Fatalf("UpsertSignInPolicy calls = %d, want 0 for a scope-denied caller", store.calls)
			}
			if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionDenied ||
				audit.events[0].ReasonCode != tc.reason {
				t.Fatalf("audit events = %#v, want exactly one denied %q event", audit.events, tc.reason)
			}
		})
	}
}

// TestSignInPolicyWriterWireNilStoreMapsTo503 proves the fail-closed leg the
// pin record names: a nil admission port answers 503 with the fixed message
// and a denied sign_in_policy_store_unavailable audit event, never a panic.
func TestSignInPolicyWriterWireNilStoreMapsTo503(t *testing.T) {
	t.Parallel()

	audit := &fakeSignInPolicyAudit{}
	rec := serveWriterPinPatch(t, nil, audit, adminSignInPolicyRequest(`{}`))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if got := rec.Body.String(); !strings.Contains(got, "sign-in policy store is unavailable") {
		t.Fatalf("body = %q, want it to name the unavailable sign-in policy store", got)
	}
	if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionDenied ||
		audit.events[0].ReasonCode != "sign_in_policy_store_unavailable" {
		t.Fatalf("audit events = %#v, want exactly one denied sign_in_policy_store_unavailable event", audit.events)
	}
}

// TestSignInPolicyWriterWireUnexpectedStoreErrorMapsTo500 proves a store
// failure that is neither a guardrail nor a timeout-ordering rejection fails
// closed with 500 and a denied sign_in_policy_update_failed audit event, and
// never leaks the store error into the body.
func TestSignInPolicyWriterWireUnexpectedStoreErrorMapsTo500(t *testing.T) {
	t.Parallel()

	store := &writerPinSignInPolicyStore{err: errors.New("disk on fire")}
	audit := &fakeSignInPolicyAudit{}
	rec := serveWriterPinPatch(t, store, audit, adminSignInPolicyRequest(`{"require_sso":true}`))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "disk on fire") {
		t.Fatalf("body leaked the store error: %s", rec.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].Decision != governanceaudit.DecisionDenied ||
		audit.events[0].ReasonCode != "sign_in_policy_update_failed" {
		t.Fatalf("audit events = %#v, want exactly one denied sign_in_policy_update_failed event", audit.events)
	}
}
