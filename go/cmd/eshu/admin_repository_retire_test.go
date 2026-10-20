// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAdminRepositoryRetirePrintsGeneratedKey pins the #8602 P1 fix: when
// --idempotency-key is blank, `eshu admin repository retire` mints the key
// before the POST and prints it to stderr, so an operator whose call fails
// after the fence commits can retry with the same key instead of minting a
// fresh one into already_open.
func TestAdminRepositoryRetirePrintsGeneratedKey(t *testing.T) {
	var bodies []map[string]any
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		bodies = append(bodies, body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ESHU_SERVICE_URL", server.URL)

	cmd := newAdminRepositoryRetireCmd()
	if err := cmd.ParseFlags([]string{"--repository", "acme/api", "--reason", "decommissioned upstream"}); err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	if err := runAdminRepositoryRetire(cmd, nil); err != nil {
		t.Fatalf("runAdminRepositoryRetire() error = %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("calls = %d, want 1", len(bodies))
	}
	if paths[0] != "/api/v0/admin/repository-retirements" {
		t.Fatalf("path = %q, want /api/v0/admin/repository-retirements", paths[0])
	}
	posted, ok := bodies[0]["idempotency_key"].(string)
	if !ok || !strings.HasPrefix(posted, "cli-retire-") {
		t.Fatalf("posted idempotency_key = %v, want the cli-retire- prefix", bodies[0]["idempotency_key"])
	}
	if !strings.Contains(stderr.String(), posted) {
		t.Fatalf("stderr = %q, want it to name the posted key %q", stderr.String(), posted)
	}
}
