// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

// TestServiceURLEnvRetargetsDefaultClient proves ESHU_SERVICE_URL moves the
// default client every no-flag call site builds (runList, runStats,
// runDoctor, the --core-base-url fallback): the drain needs no per-site
// edit because all of them funnel through NewAPIClient.
func TestServiceURLEnvRetargetsDefaultClient(t *testing.T) {
	t.Setenv("ESHU_HOME", t.TempDir())
	t.Setenv("ESHU_SERVICE_URL", "http://retarget.example:9200")
	t.Setenv("ESHU_SERVICE_URL_STAGING", "")

	if got := NewAPIClient("", "", "").BaseURL; got != "http://retarget.example:9200" {
		t.Fatalf(`NewAPIClient("","","").BaseURL = %q, want ESHU_SERVICE_URL value`, got)
	}
}

// TestServiceURLEnvRetargetsCommandClient proves apiClientFromCmd — the
// funnel behind analyze, basic query, change_impact, ecosystem, find,
// first_run, hosted, mcp_setup, and workspace — resolves the same override
// when no --service-url flag is given.
func TestServiceURLEnvRetargetsCommandClient(t *testing.T) {
	t.Setenv("ESHU_HOME", t.TempDir())
	t.Setenv("ESHU_SERVICE_URL", "http://retarget.example:9200")
	t.Setenv("ESHU_SERVICE_URL_STAGING", "")

	if got := apiClientFromCmd(newRemoteFlagsTestCommand()).BaseURL; got != "http://retarget.example:9200" {
		t.Fatalf("apiClientFromCmd().BaseURL = %q, want ESHU_SERVICE_URL value", got)
	}
}

// TestServiceURLFlagWinsOverEnv proves an explicit --service-url still beats
// the environment, so a pointed operator can override the drained default.
func TestServiceURLFlagWinsOverEnv(t *testing.T) {
	t.Setenv("ESHU_HOME", t.TempDir())
	t.Setenv("ESHU_SERVICE_URL", "http://retarget.example:9200")
	t.Setenv("ESHU_SERVICE_URL_STAGING", "")

	cmd := newRemoteFlagsTestCommand()
	if err := cmd.Flags().Set("service-url", "http://flag.example:9300"); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}
	if got := apiClientFromCmd(cmd).BaseURL; got != "http://flag.example:9300" {
		t.Fatalf("apiClientFromCmd().BaseURL = %q, want --service-url value", got)
	}
}

// TestServiceURLEnvRetargetsLiveCall proves the override reaches the wire:
// a client built with no flags against ESHU_SERVICE_URL issues its request
// to the overridden server.
func TestServiceURLEnvRetargetsLiveCall(t *testing.T) {
	t.Setenv("ESHU_HOME", t.TempDir())

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()
	t.Setenv("ESHU_SERVICE_URL", srv.URL)
	t.Setenv("ESHU_SERVICE_URL_STAGING", "")

	client := apiClientFromCmd(newRemoteFlagsTestCommand())
	var result map[string]string
	if err := client.Get("/api/v0/health", &result); err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	if gotPath != "/api/v0/health" {
		t.Fatalf("server saw path %q, want /api/v0/health", gotPath)
	}
	if result["status"] != "ok" {
		t.Fatalf("result = %#v, want status ok", result)
	}
}

// newRemoteFlagsTestCommand builds a standalone command carrying the remote
// flags apiClientFromCmd reads, without reaching the shared command tree.
func newRemoteFlagsTestCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("service-url", "", "")
	cmd.Flags().String("api-key", "", "")
	cmd.Flags().String("profile", "", "")
	return cmd
}
