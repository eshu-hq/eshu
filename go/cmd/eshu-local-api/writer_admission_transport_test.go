// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWriterAdmissionTransportFailsClosedWithoutDatabase proves the
// WithReadStore-negative leg of the W7 writer record (Refs eshu-hq/eshu#9669):
// the sign-in-policy mutation and setup wizard handlers are built with the
// versioned admission contract alone — no *sql.DB, no keyring, no
// WithReadStore-constructed reader anywhere on the path — and fail closed
// with 503 instead of panicking when that material is absent.
func TestWriterAdmissionTransportFailsClosedWithoutDatabase(t *testing.T) {
	t.Parallel()

	policyHandler := newSignInPolicyMutationHandler(nil, nil, nil)
	if policyHandler == nil {
		t.Fatal("newSignInPolicyMutationHandler(nil, nil, nil) = nil, want a handler with a nil store")
	}
	policyMux := http.NewServeMux()
	policyHandler.Mount(policyMux)
	policyRec := httptest.NewRecorder()
	policyMux.ServeHTTP(policyRec, httptest.NewRequest(
		http.MethodPatch, "/api/v0/auth/admin/sign-in-policy", bytes.NewBufferString(`{}`)))
	if policyRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("PATCH status = %d, want %d: %s", policyRec.Code, http.StatusServiceUnavailable, policyRec.Body.String())
	}
	if got := policyRec.Body.String(); !strings.Contains(got, "sign-in policy store is unavailable") {
		t.Fatalf("PATCH body = %q, want the fixed unavailable-store message", got)
	}

	setupHandler := newSetupHandler(nil, nil, nil, nil, "", "")
	if setupHandler == nil {
		t.Fatal("newSetupHandler(nil, ...) = nil, want a handler with a nil store")
	}
	if setupHandler.Store != nil {
		t.Fatalf("setup handler Store = %T(%v), want nil for the 503 path", setupHandler.Store, setupHandler.Store)
	}
	setupMux := http.NewServeMux()
	setupHandler.Mount(setupMux)
	setupRec := httptest.NewRecorder()
	setupMux.ServeHTTP(setupRec, httptest.NewRequest(http.MethodGet, "/api/v0/auth/setup-state", nil))
	if setupRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("setup-state status = %d, want %d: %s", setupRec.Code, http.StatusServiceUnavailable, setupRec.Body.String())
	}
	if got := setupRec.Body.String(); !strings.Contains(got, "setup store is unavailable") {
		t.Fatalf("setup-state body = %q, want the fixed unavailable-store message", got)
	}
}
