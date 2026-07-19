// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunFindNamePreservesLegacyGraphResolveRoute(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if got := r.Header.Get("Authorization"); got != "Bearer scoped-token" {
			t.Fatalf("Authorization = %q, want scoped bearer token", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_, _ = w.Write([]byte(`{"entities":[{"id":"repository:r_a","name":"Server","labels":["Repository"]}],"count":1,"limit":10,"truncated":false}`))
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addRemoteFlags(cmd)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v", err)
	}
	if err := cmd.Flags().Set("api-key", "scoped-token"); err != nil {
		t.Fatalf("Set(api-key) error = %v", err)
	}
	if err := runFindName(cmd, []string{"Server"}); err != nil {
		t.Fatalf("runFindName() error = %v", err)
	}
	if gotPath != "/api/v0/entities/resolve" {
		t.Fatalf("request path = %q, want legacy graph resolver", gotPath)
	}
	if len(gotBody) != 1 || gotBody["name"] != "Server" {
		t.Fatalf("request body = %#v, want name-only legacy graph lookup", gotBody)
	}
}

func TestRunFindNameDoesNotWidenDomainAfterGlobalRefusal(t *testing.T) {
	t.Parallel()

	requests := make([]string, 0, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		http.Error(w, "global entity resolution requires type or repo_id", http.StatusBadRequest)
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addRemoteFlags(cmd)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v", err)
	}
	err := runFindName(cmd, []string{"Server"})
	var apiErr *apiHTTPError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("runFindName() error = %v, want preserved 400 API error", err)
	}
	if len(requests) != 1 || requests[0] != "/api/v0/entities/resolve" {
		t.Fatalf("request paths = %v, want one fail-closed graph resolver call", requests)
	}
}

func TestRunFindPatternPostsMinimalSearchBody(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("json.Decode() error = %v, want nil", err)
		}
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addRemoteFlags(cmd)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}

	if err := runFindPattern(cmd, []string{"Search"}); err != nil {
		t.Fatalf("runFindPattern() error = %v, want nil", err)
	}
	if got, want := gotPath, "/api/v0/code/search"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
	if got, want := gotBody["query"], "Search"; got != want {
		t.Fatalf("body[query] = %#v, want %#v", got, want)
	}
	if _, ok := gotBody["search_type"]; ok {
		t.Fatalf("body[search_type] = %#v, want omitted", gotBody["search_type"])
	}
}

func TestRunFindContentPostsCrossRepoSearchBody(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("json.Decode() error = %v, want nil", err)
		}
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	cmd := &cobra.Command{}
	addRemoteFlags(cmd)
	if err := cmd.Flags().Set("service-url", server.URL); err != nil {
		t.Fatalf("Set(service-url) error = %v, want nil", err)
	}

	if err := runFindContent(cmd, []string{"sample-service"}); err != nil {
		t.Fatalf("runFindContent() error = %v, want nil", err)
	}
	if got, want := gotPath, "/api/v0/content/entities/search"; got != want {
		t.Fatalf("request path = %q, want %q", got, want)
	}
	if got, want := gotBody["query"], "sample-service"; got != want {
		t.Fatalf("body[query] = %#v, want %#v", got, want)
	}
	if _, ok := gotBody["repo_id"]; ok {
		t.Fatalf("body[repo_id] = %#v, want omitted for cross-repo content search", gotBody["repo_id"])
	}
}
