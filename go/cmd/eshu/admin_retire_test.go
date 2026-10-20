// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// TestAdminRetireRepositoriesFlags pins `eshu admin retire-repositories`:
// repeated --repository flags, --reason, and --idempotency-key post the
// trigger body, and a missing --reason fails before any HTTP call.
func TestAdminRetireRepositoriesFlags(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/admin/repositories/retire" {
			t.Errorf("path = %q, want /api/v0/admin/repositories/retire", r.URL.Path)
		}
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

	cmd := newAdminRetireRepositoriesCmd()
	if err := cmd.ParseFlags([]string{
		"--repository", "acme/api",
		"--repository", "acme/web",
		"--reason", "decommissioned upstream",
		"--idempotency-key", "retire-9",
	}); err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	if err := runAdminRetireRepositories(cmd, nil); err != nil {
		t.Fatalf("runAdminRetireRepositories() error = %v", err)
	}
	want := map[string]any{
		"repositories":    []any{"acme/api", "acme/web"},
		"reason":          "decommissioned upstream",
		"idempotency_key": "retire-9",
	}
	if got := bodies[len(bodies)-1]; !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %#v, want %#v", got, want)
	}

	missing := newAdminRetireRepositoriesCmd()
	if err := missing.ParseFlags([]string{"--repository", "acme/api"}); err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	calls := len(bodies)
	err := runAdminRetireRepositories(missing, nil)
	if err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("error = %v, want the --reason flag named", err)
	}
	if len(bodies) != calls {
		t.Fatal("an HTTP call ran with a missing --reason")
	}
}

// TestAdminRetireRepositoriesPrintsGeneratedKey proves the cobra wrapper
// mints the idempotency key before the POST and prints it to stderr, so an
// operator whose call fails after the fence commits can retry with the
// same key instead of minting a fresh one into already_open.
func TestAdminRetireRepositoriesPrintsGeneratedKey(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	cmd := newAdminRetireRepositoriesCmd()
	if err := cmd.ParseFlags([]string{"--repository", "acme/api", "--reason", "decommissioned upstream"}); err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	if err := runAdminRetireRepositories(cmd, nil); err != nil {
		t.Fatalf("runAdminRetireRepositories() error = %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("calls = %d, want 1", len(bodies))
	}
	posted, ok := bodies[0]["idempotency_key"].(string)
	if !ok || !strings.HasPrefix(posted, "cli-retire-") {
		t.Fatalf("posted idempotency_key = %v, want the cli-retire- prefix", bodies[0]["idempotency_key"])
	}
	if !strings.Contains(stderr.String(), posted) {
		t.Fatalf("stderr = %q, want it to name the posted key %q", stderr.String(), posted)
	}
}
