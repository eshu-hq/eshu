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

// TestSearchIssuesTruncationReflectsProviderPages proves the cap signal: a
// stubbed provider that fills the issue cap while further pages exist (a
// next page token with IsLast=false) reports truncated, while reaching the
// exact limit on the last page does not. A token alone is not enough —
// IsLast decides.
func TestSearchIssuesTruncationReflectsProviderPages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		nextPageToken string
		isLast        bool
		wantTruncated bool
	}{
		{"cap hit with further pages", "page-2", false, true},
		{"exact limit on last page", "", true, false},
		{"stale token on last page", "page-2", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			now := testObservedAt()
			searchCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != jiraSearchEndpoint {
					t.Errorf("request = %s %s, want POST %s", r.Method, r.URL.Path, jiraSearchEndpoint)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				searchCalls++
				issue := func(id, key, summary string) map[string]any {
					return map[string]any{
						"id":   id,
						"key":  key,
						"self": serverURL(r) + "/rest/api/3/issue/" + id,
						"fields": map[string]any{
							"summary":   summary,
							"created":   now.Add(-2 * time.Hour).Format(jiraTimeLayout),
							"updated":   now.Format(jiraTimeLayout),
							"issuetype": map[string]any{"id": "10002", "name": "Incident"},
							"status":    map[string]any{"id": "3", "name": "In Progress"},
							"project":   map[string]any{"id": "10000", "key": "OPS", "name": "Operations"},
						},
					}
				}
				writeJSON(t, w, map[string]any{
					"isLast":        tt.isLast,
					"nextPageToken": tt.nextPageToken,
					"issues": []map[string]any{
						issue("10001", "OPS-123", "Checkout alert"),
						issue("10002", "OPS-124", "Recovery follow-up"),
					},
				})
			}))
			defer server.Close()
			client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "fixture-token", Client: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			window := CollectionWindow{Since: now.Add(-time.Hour), Until: now}
			var stats CollectionStats
			issues, truncated, err := client.searchIssues(context.Background(), TargetConfig{IssueLimit: 2}, window, &stats)
			if err != nil {
				t.Fatalf("searchIssues() error = %v", err)
			}
			if len(issues) != 2 {
				t.Fatalf("len(issues) = %d, want 2", len(issues))
			}
			if truncated != tt.wantTruncated {
				t.Fatalf("searchIssues() truncated = %v, want %v (nextPageToken=%q isLast=%v)", truncated, tt.wantTruncated, tt.nextPageToken, tt.isLast)
			}
			if searchCalls != 1 {
				t.Fatalf("search requests = %d, want 1: the cap must end the walk without a follow-up page", searchCalls)
			}
			if stats.SearchPages != 1 {
				t.Fatalf("SearchPages = %d, want 1", stats.SearchPages)
			}
		})
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
