// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// An order-only target must not become a parenthesized filter expression.
func TestBoundedJQLPreservesOrderOnlyTargets(t *testing.T) {
	t.Parallel()
	window := CollectionWindow{
		Since: time.Date(2026, time.May, 31, 17, 0, 0, 0, time.UTC),
		Until: time.Date(2026, time.May, 31, 18, 0, 0, 0, time.UTC),
	}
	windowClause := `updated >= "2026-05-31 17:00" AND updated <= "2026-05-31 18:00"`
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"deployed order only", "ORDER BY updated DESC", windowClause + " ORDER BY updated DESC"},
		{"case and outer whitespace", " \torder by priority DESC\n", windowClause + " order by priority DESC"},
		{"empty", "", windowClause + " ORDER BY updated ASC"},
		{"whitespace", " \t\n", windowClause + " ORDER BY updated ASC"},
		{"filter with order", "project = OPS ORDER BY priority DESC", "(project = OPS) AND " + windowClause + " ORDER BY priority DESC"},
		{"filter without order", "project = OPS", "(project = OPS) AND " + windowClause + " ORDER BY updated ASC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := boundedJQL(tt.raw, window); got != tt.want {
				t.Fatalf("boundedJQL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// Exercise the production POST boundary, including successful empty windows.
func TestSearchIssuesAcceptsOrderOnlyTarget(t *testing.T) {
	t.Parallel()
	window := CollectionWindow{Since: testObservedAt().Add(-time.Hour), Until: testObservedAt()}
	want := `updated >= "` + window.Since.UTC().Format(jiraJQLTimeLayout) + `" AND updated <= "` + window.Until.UTC().Format(jiraJQLTimeLayout) + `" ORDER BY updated DESC`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != jiraSearchEndpoint {
			t.Errorf("request = %s %s, want POST %s", r.Method, r.URL.Path, jiraSearchEndpoint)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body searchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode search request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if body.JQL != want {
			t.Errorf("search JQL = %q, want %q", body.JQL, want)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"issues":[],"isLast":true}`))
	}))
	defer server.Close()
	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "fixture-token", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	var stats CollectionStats
	issues, truncated, err := client.searchIssues(context.Background(), TargetConfig{JQL: "ORDER BY updated DESC", IssueLimit: 1}, window, &stats)
	if err != nil {
		t.Fatalf("searchIssues() error = %v", err)
	}
	if len(issues) != 0 || stats.SearchPages != 1 {
		t.Fatalf("issues = %d, search pages = %d; want 0 and 1", len(issues), stats.SearchPages)
	}
	if truncated {
		t.Fatal("searchIssues() truncated = true for an exhausted provider, want false")
	}
}

// Compare existing filtered targets without adding provider or network work.
func BenchmarkBoundedJQLFilteredTarget(b *testing.B) {
	window := CollectionWindow{Since: testObservedAt().Add(-time.Hour), Until: testObservedAt()}
	b.ReportAllocs()
	for b.Loop() {
		_ = boundedJQL("project = OPS ORDER BY priority DESC", window)
	}
}
