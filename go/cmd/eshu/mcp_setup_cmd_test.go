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
	addRemoteFlags(cmd)
	return cmd
}

func TestRunMCPSetupHostedDoesNotPrintRawToken(t *testing.T) {
	cmd := newSetupCmd()
	if err := cmd.Flags().Set("platform", "claude"); err != nil {
		t.Fatalf("set platform: %v", err)
	}
	if err := cmd.Flags().Set("hosted", "true"); err != nil {
		t.Fatalf("set hosted: %v", err)
	}
	if err := cmd.Flags().Set("service-url", "https://eshu.example.com"); err != nil {
		t.Fatalf("set service-url: %v", err)
	}
	if err := cmd.Flags().Set("api-key", fakeBearerToken); err != nil {
		t.Fatalf("set api-key: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runMCPSetup(cmd, nil); err != nil {
			t.Fatalf("runMCPSetup error = %v, want nil", err)
		}
	})
	if strings.Contains(out, fakeBearerToken) {
		t.Fatalf("hosted setup leaked raw token:\n%s", out)
	}
	if !strings.Contains(out, "${"+apiKeyEnvVar+"}") {
		t.Fatalf("hosted setup did not emit env-var reference:\n%s", out)
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
}
