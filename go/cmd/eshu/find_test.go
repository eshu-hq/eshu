package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

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
