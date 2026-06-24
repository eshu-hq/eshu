// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newTestFreshnessGenerationsCommand() *cobra.Command {
	cmd := &cobra.Command{}
	addFreshnessGenerationsFlags(cmd)
	addRemoteFlags(cmd)
	return cmd
}

func stubFreshnessFetch(t *testing.T, envelope freshnessGenerationsEnvelope, err error) func() {
	t.Helper()
	original := freshnessFetchGenerations
	freshnessFetchGenerations = func(_ *APIClient, _ freshnessGenerationsOptions) (freshnessGenerationsEnvelope, error) {
		return envelope, err
	}
	return func() { freshnessFetchGenerations = original }
}

func TestFreshnessGenerationsCommandIsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"freshness", "generations"})
	if err != nil {
		t.Fatalf("rootCmd.Find(freshness generations) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "generations" {
		t.Fatalf("resolved command = %#v, want generations", cmd)
	}
	for _, name := range []string{"json", "scope-id", "repository", "collector-kind", "source-system", "generation-id", "status", "limit", "service-url"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("freshness generations flag %q missing", name)
		}
	}
}

func TestFetchFreshnessGenerationsRequestsCanonicalEnvelope(t *testing.T) {
	var gotAccept string
	var gotPath string
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.EscapedPath()
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"count":0,"truncated":false,"generations":[]},"truth":{"freshness":{"state":"fresh"}},"error":null}`))
	}))
	defer server.Close()

	client := &APIClient{BaseURL: server.URL, HTTPClient: server.Client()}
	if _, err := fetchFreshnessGenerations(client, freshnessGenerationsOptions{
		ScopeID: "git-repository-scope:acme/app",
		Status:  "active",
		Limit:   25,
	}); err != nil {
		t.Fatalf("fetchFreshnessGenerations() error = %v", err)
	}
	if gotAccept != eshuEnvelopeMIMEType {
		t.Fatalf("Accept = %q, want %q", gotAccept, eshuEnvelopeMIMEType)
	}
	if gotPath != "/api/v0/freshness/generations" {
		t.Fatalf("path = %q", gotPath)
	}
	for key, want := range map[string]string{
		"scope_id": "git-repository-scope:acme/app",
		"status":   "active",
		"limit":    "25",
	} {
		if got := gotQuery.Get(key); got != want {
			t.Fatalf("query[%s] = %q, want %q", key, got, want)
		}
	}
}

func TestRunFreshnessGenerationsRendersSummary(t *testing.T) {
	reset := stubFreshnessFetch(t, freshnessGenerationsEnvelope{
		Data: map[string]any{
			"count":     float64(1),
			"truncated": false,
			"generations": []any{map[string]any{
				"generation_id": "gen-active",
				"status":        "active",
				"scope_id":      "git-repository-scope:acme/app",
				"trigger_kind":  "snapshot",
				"is_active":     true,
				"queue_status":  map[string]any{"outstanding": float64(0), "failed": float64(0), "dead_letter": float64(0)},
			}},
		},
		Truth: map[string]any{"freshness": map[string]any{"state": "fresh"}},
	}, nil)
	defer reset()

	out := &bytes.Buffer{}
	cmd := newTestFreshnessGenerationsCommand()
	cmd.SetOut(out)

	if err := runFreshnessGenerations(cmd, nil); err != nil {
		t.Fatalf("runFreshnessGenerations() error = %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "gen-active") || !strings.Contains(output, "status=active") {
		t.Fatalf("summary missing generation row: %q", output)
	}
	if !strings.Contains(output, "Truth freshness: fresh") {
		t.Fatalf("summary missing freshness: %q", output)
	}
}

func TestRunFreshnessGenerationsNotFoundExits(t *testing.T) {
	reset := stubFreshnessFetch(t, freshnessGenerationsEnvelope{
		Error: &freshnessGenerationError{Code: "scope_not_found", Message: "no records for scope"},
	}, nil)
	defer reset()

	out := &bytes.Buffer{}
	cmd := newTestFreshnessGenerationsCommand()
	cmd.SetOut(out)

	err := runFreshnessGenerations(cmd, nil)
	if err == nil {
		t.Fatal("runFreshnessGenerations() error = nil, want not-found exit")
	}
	var exitErr commandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T, want commandExitError", err)
	}
	if got, want := exitErr.ExitCode(), 2; got != want {
		t.Fatalf("ExitCode() = %d, want %d", got, want)
	}
}
