// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// verifyPostureServer records what the auth-enforced query route
// (/api/v0/index-status) received so a test can prove which credential
// --verify actually presented. /health is public and always 200s;
// index-status can be configured to 401 (SSO's no-bearer case) or 200.
type verifyPostureServer struct {
	server          *httptest.Server
	indexStatusHits int
	indexStatusAuth string
}

// newVerifyPostureServer starts an httptest server. When indexStatus401 is
// true the auth-enforced query route answers 401 (modeling the SSO/no-bearer
// case where the CLI holds no token); otherwise it answers 200 and records
// the inbound Authorization header. The discovery well-known route always
// 404s so the "auto" posture would fall back to token -- tests that need a
// specific posture pass --auth explicitly.
func newVerifyPostureServer(t *testing.T, indexStatus401 bool) *verifyPostureServer {
	t.Helper()
	vps := &verifyPostureServer{}
	vps.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case strings.HasPrefix(r.URL.Path, "/api/v0/index-status"):
			vps.indexStatusHits++
			vps.indexStatusAuth = r.Header.Get("Authorization")
			if indexStatus401 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"status":"ready"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(vps.server.Close)
	return vps
}

// TestRunMCPSetupVerifyTokenPosturePrefersMcpToken proves P1 #1: token-posture
// --verify exercises ${ESHU_MCP_TOKEN}, not a resolved shared ESHU_API_KEY.
// With both a shared key (via --api-key, standing in for a resolved
// ESHU_API_KEY) and a personal ESHU_MCP_TOKEN present, the auth-enforced query
// route must receive the personal token -- the credential the emitted snippet
// actually wires -- so --verify cannot give false confidence in the shared key.
func TestRunMCPSetupVerifyTokenPosturePrefersMcpToken(t *testing.T) {
	const sharedKey = "eshu_shared_ADMIN_key_TESTONLY"       // #nosec G101 -- test fixture value, not a real secret
	const personalToken = "eshu_pat_PERSONAL_token_TESTONLY" // #nosec G101 -- test fixture value, not a real secret
	t.Setenv(mcpTokenEnvVar, personalToken)

	vps := newVerifyPostureServer(t, false)
	cmd := newSetupCmd()
	setCmdFlags(t, cmd, map[string]string{
		"hosted":      "true",
		"verify":      "true",
		"service-url": vps.server.URL,
		"api-key":     sharedKey,
		"auth":        "token",
	})

	out := captureStdout(t, func() {
		if err := runMCPSetup(cmd, nil); err != nil {
			t.Fatalf("runMCPSetup --verify error = %v, want nil", err)
		}
	})
	if vps.indexStatusHits == 0 {
		t.Fatal("token posture with ESHU_MCP_TOKEN set should probe the auth-enforced query route")
	}
	if vps.indexStatusAuth != "Bearer "+personalToken {
		t.Fatalf("query route Authorization = %q, want the personal token %q (never the shared key)", vps.indexStatusAuth, "Bearer "+personalToken)
	}
	if strings.Contains(vps.indexStatusAuth, sharedKey) {
		t.Fatalf("token posture must not present the shared key to the auth-enforced route; got %q", vps.indexStatusAuth)
	}
	if !strings.Contains(out, "Auth posture: token") {
		t.Fatalf("verify output missing token posture line:\n%s", out)
	}
}

// TestRunMCPSetupVerifyTokenPostureNoTokenSkipsQuery proves the token-posture
// branch of P1 #1's fix: with no ESHU_MCP_TOKEN, --verify must run only the
// public health stage and SKIP the auth-enforced query (never fall back to
// probing the shared key). The report stays allOK (skip, not fail) and the
// skip reason names ESHU_MCP_TOKEN.
func TestRunMCPSetupVerifyTokenPostureNoTokenSkipsQuery(t *testing.T) {
	const sharedKey = "eshu_shared_ADMIN_key_TESTONLY" // #nosec G101 -- test fixture value, not a real secret
	t.Setenv(mcpTokenEnvVar, "")                       // explicitly unset the personal token

	vps := newVerifyPostureServer(t, false)
	cmd := newSetupCmd()
	setCmdFlags(t, cmd, map[string]string{
		"hosted":      "true",
		"verify":      "true",
		"service-url": vps.server.URL,
		"api-key":     sharedKey,
		"auth":        "token",
	})

	out := captureStdout(t, func() {
		if err := runMCPSetup(cmd, nil); err != nil {
			t.Fatalf("runMCPSetup --verify error = %v, want nil (skipped query is not a failure)", err)
		}
	})
	if vps.indexStatusHits != 0 {
		t.Fatalf("token posture without ESHU_MCP_TOKEN must not probe the auth-enforced route (shared key leak); hits=%d auth=%q", vps.indexStatusHits, vps.indexStatusAuth)
	}
	if !strings.Contains(out, mcpTokenEnvVar) {
		t.Fatalf("skipped-query reason should name %s:\n%s", mcpTokenEnvVar, out)
	}
	if !strings.Contains(out, "[--] first query successful") {
		t.Fatalf("first-query stage should be marked skipped ([--]):\n%s", out)
	}
}

// TestRunMCPSetupVerifySSOSkipsQuery proves P1 #2: SSO-posture --verify keeps
// the public health reachability stage but SKIPS the auth-enforced query,
// which would 401 (OAuth is interactive; the CLI holds no bearer). The overall
// report must be allOK (skip, not fail) and the skip reason must mention the
// interactive OAuth flow.
func TestRunMCPSetupVerifySSOSkipsQuery(t *testing.T) {
	vps := newVerifyPostureServer(t, true) // index-status 401s
	cmd := newSetupCmd()
	setCmdFlags(t, cmd, map[string]string{
		"hosted":      "true",
		"verify":      "true",
		"service-url": vps.server.URL,
		"auth":        "sso",
	})

	out := captureStdout(t, func() {
		if err := runMCPSetup(cmd, nil); err != nil {
			t.Fatalf("runMCPSetup --verify (SSO) error = %v, want nil (query skipped, health OK)", err)
		}
	})
	if vps.indexStatusHits != 0 {
		t.Fatalf("SSO posture must not probe the auth-enforced query route (it would 401); hits=%d", vps.indexStatusHits)
	}
	if !strings.Contains(out, "[ok] client reachable") {
		t.Fatalf("SSO posture should still run the public health reachability stage:\n%s", out)
	}
	if !strings.Contains(out, "[--] first query successful") {
		t.Fatalf("SSO posture first-query stage should be marked skipped ([--]):\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "oauth") {
		t.Fatalf("SSO skip reason should mention the interactive OAuth flow:\n%s", out)
	}
}

// TestRunMCPSetupVerifySharedKeyRunsQuery proves the shared-key posture is
// unchanged by the fix: the shared key is present, so both the health and the
// auth-enforced query stages run.
func TestRunMCPSetupVerifySharedKeyRunsQuery(t *testing.T) {
	const sharedKey = "eshu_shared_ADMIN_key_TESTONLY" // #nosec G101 -- test fixture value, not a real secret

	vps := newVerifyPostureServer(t, false)
	cmd := newSetupCmd()
	setCmdFlags(t, cmd, map[string]string{
		"hosted":      "true",
		"verify":      "true",
		"service-url": vps.server.URL,
		"api-key":     sharedKey,
		"auth":        "shared-key",
	})

	out := captureStdout(t, func() {
		if err := runMCPSetup(cmd, nil); err != nil {
			t.Fatalf("runMCPSetup --verify (shared-key) error = %v, want nil", err)
		}
	})
	if vps.indexStatusHits == 0 {
		t.Fatal("shared-key posture should probe the auth-enforced query route")
	}
	if vps.indexStatusAuth != "Bearer "+sharedKey {
		t.Fatalf("shared-key query Authorization = %q, want %q", vps.indexStatusAuth, "Bearer "+sharedKey)
	}
	if !strings.Contains(out, "[ok] first query successful") {
		t.Fatalf("shared-key posture first-query stage should run and pass:\n%s", out)
	}
}
