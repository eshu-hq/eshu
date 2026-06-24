// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFreshnessServiceChangedSinceCommandIsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"freshness", "service-changed-since"})
	if err != nil {
		t.Fatalf("rootCmd.Find(freshness service-changed-since) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "service-changed-since" {
		t.Fatalf("resolved command = %#v, want service-changed-since", cmd)
	}
	for _, name := range []string{"json", "service-id", "since-generation-id", "sample-limit", "service-url"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("freshness service-changed-since flag %q missing", name)
		}
	}
}

func TestFetchFreshnessServiceChangedSinceRequestsCanonicalEnvelope(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"service_id":"svc-a","categories":[]},"truth":{"freshness":{"state":"fresh"}},"error":null}`))
	}))
	defer server.Close()

	client := &APIClient{BaseURL: server.URL, HTTPClient: server.Client()}
	if _, err := fetchFreshnessServiceChangedSince(client, freshnessServiceChangedSinceOptions{
		ServiceID:         "svc-a",
		SinceGenerationID: "gen-prior",
		SampleLimit:       40,
	}); err != nil {
		t.Fatalf("fetchFreshnessServiceChangedSince() error = %v", err)
	}
	if gotPath != "/api/v0/freshness/services/changed-since" {
		t.Fatalf("path = %q", gotPath)
	}
	for key, want := range map[string]string{
		"service_id":          "svc-a",
		"since_generation_id": "gen-prior",
		"sample_limit":        "40",
	} {
		if got := gotQuery.Get(key); got != want {
			t.Fatalf("query[%s] = %q, want %q", key, got, want)
		}
	}
}
