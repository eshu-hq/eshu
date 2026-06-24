// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func stubFreshnessChangedSinceFetch(t *testing.T, envelope freshnessGenerationsEnvelope, err error) func() {
	t.Helper()
	original := freshnessFetchChangedSince
	freshnessFetchChangedSince = func(_ *APIClient, _ freshnessChangedSinceOptions) (freshnessGenerationsEnvelope, error) {
		return envelope, err
	}
	return func() { freshnessFetchChangedSince = original }
}

func TestFreshnessChangedSinceCommandIsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"freshness", "changed-since"})
	if err != nil {
		t.Fatalf("rootCmd.Find(freshness changed-since) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "changed-since" {
		t.Fatalf("resolved command = %#v, want changed-since", cmd)
	}
	for _, name := range []string{"json", "scope-id", "repository", "since-generation-id", "since-observed-at", "sample-limit", "service-url"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("freshness changed-since flag %q missing", name)
		}
	}
}

func TestFetchFreshnessChangedSinceRequestsCanonicalEnvelope(t *testing.T) {
	var gotAccept string
	var gotPath string
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.EscapedPath()
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"scope_id":"s","categories":[]},"truth":{"freshness":{"state":"fresh"}},"error":null}`))
	}))
	defer server.Close()

	client := &APIClient{BaseURL: server.URL, HTTPClient: server.Client()}
	if _, err := fetchFreshnessChangedSince(client, freshnessChangedSinceOptions{
		Repository:        "acme/app",
		SinceGenerationID: "gen-prior",
		SampleLimit:       40,
	}); err != nil {
		t.Fatalf("fetchFreshnessChangedSince() error = %v", err)
	}
	if gotAccept != eshuEnvelopeMIMEType {
		t.Fatalf("Accept = %q, want %q", gotAccept, eshuEnvelopeMIMEType)
	}
	if gotPath != "/api/v0/freshness/changed-since" {
		t.Fatalf("path = %q", gotPath)
	}
	for key, want := range map[string]string{
		"repository":          "acme/app",
		"since_generation_id": "gen-prior",
		"sample_limit":        "40",
	} {
		if got := gotQuery.Get(key); got != want {
			t.Fatalf("query[%s] = %q, want %q", key, got, want)
		}
	}
}

func TestRunFreshnessChangedSinceRendersSummary(t *testing.T) {
	reset := stubFreshnessChangedSinceFetch(t, freshnessGenerationsEnvelope{
		Data: map[string]any{
			"scope_id":                     "git-repository-scope:acme/app",
			"since_generation_id":          "gen-prior",
			"current_active_generation_id": "gen-current",
			"unavailable":                  false,
			"categories": []any{map[string]any{
				"category":    "files",
				"unavailable": false,
				"counts": map[string]any{
					"added": float64(2), "updated": float64(1), "unchanged": float64(5),
					"retired": float64(1), "superseded": float64(1),
				},
			}},
		},
		Truth: map[string]any{"freshness": map[string]any{"state": "fresh"}},
	}, nil)
	defer reset()

	out := &bytes.Buffer{}
	cmd := newFreshnessChangedSinceCommand()
	cmd.SetOut(out)

	if err := runFreshnessChangedSince(cmd, nil); err != nil {
		t.Fatalf("runFreshnessChangedSince() error = %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "gen-prior -> gen-current") {
		t.Fatalf("summary missing baseline line: %q", output)
	}
	if !strings.Contains(output, "retired=1 superseded=1") {
		t.Fatalf("summary missing retired/superseded counts: %q", output)
	}
	if !strings.Contains(output, "Truth freshness: fresh") {
		t.Fatalf("summary missing freshness: %q", output)
	}
}

func TestRunFreshnessChangedSinceUnavailableRendersNotice(t *testing.T) {
	reset := stubFreshnessChangedSinceFetch(t, freshnessGenerationsEnvelope{
		Data: map[string]any{
			"scope_id":    "git-repository-scope:acme/app",
			"unavailable": true,
			"categories":  []any{},
		},
		Truth: map[string]any{"freshness": map[string]any{"state": "unavailable"}},
	}, nil)
	defer reset()

	out := &bytes.Buffer{}
	cmd := newFreshnessChangedSinceCommand()
	cmd.SetOut(out)

	if err := runFreshnessChangedSince(cmd, nil); err != nil {
		t.Fatalf("runFreshnessChangedSince() error = %v", err)
	}
	if !strings.Contains(out.String(), "diff unavailable") {
		t.Fatalf("summary missing unavailable notice: %q", out.String())
	}
}
