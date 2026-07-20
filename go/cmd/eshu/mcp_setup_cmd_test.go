// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newSetupCmd builds a cobra command wired with the same flags as `mcp setup`
// so RunE-level behavior is exercised end to end.
func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{RunE: runMCPSetup}
	cmd.Flags().String("platform", "generic", "")
	cmd.Flags().Bool("hosted", false, "")
	cmd.Flags().Bool("write", false, "")
	cmd.Flags().String("target", "", "")
	cmd.Flags().Bool("verify", false, "")
	cmd.Flags().String("auth", "auto", "")
	cmd.Flags().Bool("shared-key", false, "")
	addRemoteFlags(cmd)
	return cmd
}

// setCmdFlags sets each named flag to its string value, failing the test on
// any error. It keeps the per-test flag wiring in the tests below terse.
func setCmdFlags(t *testing.T, cmd *cobra.Command, values map[string]string) {
	t.Helper()
	for name, value := range values {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}
}

func TestRunMCPSetupHostedTokenPostureDoesNotPrintRawToken(t *testing.T) {
	cmd := newSetupCmd()
	// auth=token is explicit so this never dials the network: an
	// unauthenticated placeholder service-url with the default "auto" would
	// otherwise trigger a real discovery probe, which is what
	// TestRunMCPSetupHostedAutoDetectsSSO/AutoFallsBackToToken cover with a
	// local httptest server instead.
	setCmdFlags(t, cmd, map[string]string{
		"platform":    "claude",
		"hosted":      "true",
		"service-url": "https://eshu.example.com",
		"api-key":     fakeBearerToken,
		"auth":        "token",
	})

	out := captureStdout(t, func() {
		if err := runMCPSetup(cmd, nil); err != nil {
			t.Fatalf("runMCPSetup error = %v, want nil", err)
		}
	})
	if strings.Contains(out, fakeBearerToken) {
		t.Fatalf("hosted token-posture setup leaked raw token:\n%s", out)
	}
	if !strings.Contains(out, "${"+mcpTokenEnvVar+"}") {
		t.Fatalf("hosted token-posture setup did not emit env-var reference:\n%s", out)
	}
	if strings.Contains(out, "${"+apiKeyEnvVar+"}") {
		t.Fatalf("hosted token-posture setup must not reference the shared key:\n%s", out)
	}
}

func TestRunMCPSetupHostedSharedKeyPostureDoesNotPrintRawToken(t *testing.T) {
	cmd := newSetupCmd()
	setCmdFlags(t, cmd, map[string]string{
		"platform":    "claude",
		"hosted":      "true",
		"service-url": "https://eshu.example.com",
		"api-key":     fakeBearerToken,
		"auth":        "shared-key",
	})

	out := captureStdout(t, func() {
		if err := runMCPSetup(cmd, nil); err != nil {
			t.Fatalf("runMCPSetup error = %v, want nil", err)
		}
	})
	if strings.Contains(out, fakeBearerToken) {
		t.Fatalf("hosted shared-key setup leaked raw token:\n%s", out)
	}
	if !strings.Contains(out, "${"+apiKeyEnvVar+"}") {
		t.Fatalf("hosted shared-key setup did not emit env-var reference:\n%s", out)
	}
	if !strings.Contains(out, "WARNING") {
		t.Fatalf("hosted shared-key setup missing admin/dev warning:\n%s", out)
	}
}

func TestRunMCPSetupHostedSharedKeyBoolFlagAlsoWorks(t *testing.T) {
	cmd := newSetupCmd()
	setCmdFlags(t, cmd, map[string]string{
		"platform":    "claude",
		"hosted":      "true",
		"service-url": "https://eshu.example.com",
		"shared-key":  "true",
	})

	out := captureStdout(t, func() {
		if err := runMCPSetup(cmd, nil); err != nil {
			t.Fatalf("runMCPSetup error = %v, want nil", err)
		}
	})
	if !strings.Contains(out, "${"+apiKeyEnvVar+"}") {
		t.Fatalf("--shared-key setup did not emit env-var reference:\n%s", out)
	}
}

// newSetupCmdWithDiscovery builds a setup command whose --service-url points
// at an httptest server so the default "auto" posture probe stays hermetic
// (local loopback only, never real network).
func newSetupCmdWithDiscovery(t *testing.T, discoveryHandler http.HandlerFunc) (*cobra.Command, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(discoveryHandler)
	t.Cleanup(server.Close)
	cmd := newSetupCmd()
	setCmdFlags(t, cmd, map[string]string{
		"platform":    "claude",
		"hosted":      "true",
		"service-url": server.URL,
	})
	return cmd, server
}

func TestRunMCPSetupHostedAutoDetectsSSO(t *testing.T) {
	cmd, _ := newSetupCmdWithDiscovery(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/oauth-protected-resource" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"resource": "https://eshu.example.com/mcp",
			"authorization_servers": ["https://idp.example.com/oauth2/aus123"]
		}`))
	})

	out := captureStdout(t, func() {
		if err := runMCPSetup(cmd, nil); err != nil {
			t.Fatalf("runMCPSetup error = %v, want nil", err)
		}
	})
	if strings.Contains(out, "${"+apiKeyEnvVar+"}") || strings.Contains(out, "${"+mcpTokenEnvVar+"}") {
		t.Fatalf("auto-detected SSO setup must not reference a bearer token env var:\n%s", out)
	}
	if !strings.Contains(out, "https://idp.example.com/oauth2/aus123") {
		t.Fatalf("auto-detected SSO setup should name the issuer:\n%s", out)
	}
	jsonPart := extractJSON(t, out)
	var doc map[string]any
	if err := json.Unmarshal([]byte(jsonPart), &doc); err != nil {
		t.Fatalf("SSO snippet not valid JSON: %v\n%s", err, jsonPart)
	}
	entry := doc["mcpServers"].(map[string]any)["eshu"].(map[string]any)
	if _, present := entry["headers"]; present {
		t.Fatalf("auto-detected SSO entry must omit headers entirely:\n%s", jsonPart)
	}
}

func TestRunMCPSetupHostedAutoFallsBackToToken(t *testing.T) {
	t.Run("404 falls back with no warning", func(t *testing.T) {
		cmd, _ := newSetupCmdWithDiscovery(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})

		var stdout, stderr string
		stderr = captureStderr(t, func() {
			stdout = captureStdout(t, func() {
				if err := runMCPSetup(cmd, nil); err != nil {
					t.Fatalf("runMCPSetup error = %v, want nil", err)
				}
			})
		})
		if !strings.Contains(stdout, "${"+mcpTokenEnvVar+"}") {
			t.Fatalf("404 fallback should emit the token env-var reference:\n%s", stdout)
		}
		if strings.TrimSpace(stderr) != "" {
			t.Fatalf("404 fallback (the documented no-SSO signal) should not warn:\n%s", stderr)
		}
	})

	t.Run("dead endpoint falls back with a warning", func(t *testing.T) {
		cmd := newSetupCmd()
		setCmdFlags(t, cmd, map[string]string{
			"platform":    "claude",
			"hosted":      "true",
			"service-url": "http://127.0.0.1:1",
		})

		var stdout, stderr string
		stderr = captureStderr(t, func() {
			stdout = captureStdout(t, func() {
				if err := runMCPSetup(cmd, nil); err != nil {
					t.Fatalf("runMCPSetup error = %v, want nil", err)
				}
			})
		})
		if !strings.Contains(stdout, "${"+mcpTokenEnvVar+"}") {
			t.Fatalf("dead-endpoint fallback should emit the token env-var reference:\n%s", stdout)
		}
		if strings.TrimSpace(stderr) == "" {
			t.Fatal("dead-endpoint fallback should print a warning")
		}
		if !strings.Contains(stderr, "--auth sso") {
			t.Fatalf("fallback warning should name the override: %q", stderr)
		}
	})
}

func TestRunMCPSetupSharedKeyRequiresExplicitFlag(t *testing.T) {
	// Default hosted run (no --auth, no --shared-key): the discovery server
	// 404s, so auto resolves to token posture. Neither an explicit flag nor
	// SSO detection is present, so the shared key must never appear.
	cmd, _ := newSetupCmdWithDiscovery(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	setCmdFlags(t, cmd, map[string]string{"api-key": fakeBearerToken})

	out := captureStdout(t, func() {
		if err := runMCPSetup(cmd, nil); err != nil {
			t.Fatalf("runMCPSetup error = %v, want nil", err)
		}
	})
	if strings.Contains(out, "${"+apiKeyEnvVar+"}") {
		t.Fatalf("default hosted run must never emit %s:\n%s", apiKeyEnvVar, out)
	}
	if strings.Contains(out, fakeBearerToken) {
		t.Fatalf("default hosted run leaked the raw shared key:\n%s", out)
	}
}

func TestRunMCPSetupWriteSSOOmitsHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"resource": "https://eshu.example.com/mcp",
			"authorization_servers": ["https://idp.example.com/oauth2/aus123"]
		}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "mcp.json")

	cmd := newSetupCmd()
	setCmdFlags(t, cmd, map[string]string{
		"platform":    "cursor",
		"hosted":      "true",
		"write":       "true",
		"target":      target,
		"service-url": server.URL,
	})

	_ = captureStdout(t, func() {
		if err := runMCPSetup(cmd, nil); err != nil {
			t.Fatalf("runMCPSetup --write error = %v, want nil", err)
		}
	})

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("written config not valid JSON: %v\n%s", err, data)
	}
	entry := doc["mcpServers"].(map[string]any)["eshu"].(map[string]any)
	if _, present := entry["headers"]; present {
		t.Fatalf("--write SSO entry must omit headers entirely:\n%s", data)
	}
}

func TestRunMCPSetupWriteMergesConfig(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(target, []byte(`{"mcpServers":{"keep":{"command":"x"}}}`), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	cmd := newSetupCmd()
	if err := cmd.Flags().Set("platform", "cursor"); err != nil {
		t.Fatalf("set platform: %v", err)
	}
	if err := cmd.Flags().Set("write", "true"); err != nil {
		t.Fatalf("set write: %v", err)
	}
	if err := cmd.Flags().Set("target", target); err != nil {
		t.Fatalf("set target: %v", err)
	}

	_ = captureStdout(t, func() {
		if err := runMCPSetup(cmd, nil); err != nil {
			t.Fatalf("runMCPSetup --write error = %v, want nil", err)
		}
	})

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("written config not valid JSON: %v\n%s", err, data)
	}
	servers := doc["mcpServers"].(map[string]any)
	if _, ok := servers["keep"].(map[string]any); !ok {
		t.Fatalf("--write dropped existing server:\n%s", data)
	}
	if _, ok := servers["eshu"].(map[string]any); !ok {
		t.Fatalf("--write missing eshu server:\n%s", data)
	}
}

func TestRunMCPSetupUnsupportedPlatform(t *testing.T) {
	t.Parallel()
	cmd := newSetupCmd()
	if err := cmd.Flags().Set("platform", "notepad"); err != nil {
		t.Fatalf("set platform: %v", err)
	}
	if err := runMCPSetup(cmd, nil); err == nil {
		t.Fatal("runMCPSetup(notepad) error = nil, want non-nil")
	}
}

func TestRunMCPSetupVerifyHostedReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case strings.HasPrefix(r.URL.Path, "/api/v0/index-status"):
			_, _ = w.Write([]byte(`{"status":"ready"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cmd := newSetupCmd()
	if err := cmd.Flags().Set("hosted", "true"); err != nil {
		t.Fatalf("set hosted: %v", err)
	}
	if err := cmd.Flags().Set("verify", "true"); err != nil {
		t.Fatalf("set verify: %v", err)
	}
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("set service-url: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runMCPSetup(cmd, nil); err != nil {
			t.Fatalf("runMCPSetup --verify error = %v, want nil", err)
		}
	})
	for _, want := range []string{"config generated", "client reachable", "tools visible", "first query successful"} {
		if !strings.Contains(out, want) {
			t.Fatalf("verify output missing stage %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "Auth posture: token") {
		t.Fatalf("verify output missing leading posture line:\n%s", out)
	}
}

// TestRunMCPSetupVerifyTokenPostureUsesEnvFallback proves --verify exercises
// the same credential the token-posture snippet wires: when no --api-key is
// resolved, the health/first-query probes fall back to ESHU_MCP_TOKEN so
// verification actually authenticates the way a real client would.
func TestRunMCPSetupVerifyTokenPostureUsesEnvFallback(t *testing.T) {
	const envToken = "eshu_pat_verify_fallback_TESTONLY" // #nosec G101 -- test-only fixture value
	t.Setenv(mcpTokenEnvVar, envToken)
	t.Setenv("ESHU_API_KEY", "")

	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case strings.HasPrefix(r.URL.Path, "/api/v0/index-status"):
			gotAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"status":"ready"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cmd := newSetupCmd()
	setCmdFlags(t, cmd, map[string]string{
		"hosted":      "true",
		"verify":      "true",
		"service-url": server.URL,
		"auth":        "token",
	})

	out := captureStdout(t, func() {
		if err := runMCPSetup(cmd, nil); err != nil {
			t.Fatalf("runMCPSetup --verify error = %v, want nil", err)
		}
	})
	if gotAuth != "Bearer "+envToken {
		t.Fatalf("index-status probe Authorization = %q, want %q", gotAuth, "Bearer "+envToken)
	}
	if !strings.Contains(out, "Auth posture: token") {
		t.Fatalf("verify output missing posture line:\n%s", out)
	}
}

// TestRunMCPSetupVerifyPostureLineByPosture pins the leading posture line's
// content for each of the three postures.
func TestRunMCPSetupVerifyPostureLineByPosture(t *testing.T) {
	tests := []struct {
		name string
		auth string
		want string
	}{
		{"token", "token", "Auth posture: token"},
		{"sso", "sso", "Auth posture: sso"},
		{"shared-key", "shared-key", "Auth posture: shared-key"},
	}
	for _, tt := range tests {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/health":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("ok"))
			case strings.HasPrefix(r.URL.Path, "/api/v0/index-status"):
				_, _ = w.Write([]byte(`{"status":"ready"}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))

		cmd := newSetupCmd()
		setCmdFlags(t, cmd, map[string]string{
			"hosted":      "true",
			"verify":      "true",
			"service-url": server.URL,
			"auth":        tt.auth,
		})

		out := captureStdout(t, func() {
			if err := runMCPSetup(cmd, nil); err != nil {
				t.Fatalf("[%s] runMCPSetup --verify error = %v, want nil", tt.name, err)
			}
		})
		server.Close()
		if !strings.Contains(out, tt.want) {
			t.Fatalf("[%s] verify output missing %q:\n%s", tt.name, tt.want, out)
		}
	}
}
