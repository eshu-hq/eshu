// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPClientCollectWorkItemEvidenceUsesBoundedJiraEndpoints(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	var requested []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Basic ") {
			t.Fatalf("Authorization = %q, want Basic auth", got)
		}
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/rest/api/3/search/jql":
			var body searchRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode search body: %v", err)
			}
			if body.MaxResults != 25 {
				t.Fatalf("maxResults = %d, want 25", body.MaxResults)
			}
			if !strings.Contains(body.JQL, "updated >=") {
				t.Fatalf("JQL = %q, want updated window", body.JQL)
			}
			writeJSON(t, w, map[string]any{
				"issues": []map[string]any{{
					"id":   "10001",
					"key":  "OPS-123",
					"self": server.URL + "/rest/api/3/issue/10001",
					"fields": map[string]any{
						"summary":   "Investigate checkout alert",
						"created":   now.Add(-2 * time.Hour).Format(jiraTimeLayout),
						"updated":   now.Format(jiraTimeLayout),
						"issuetype": map[string]any{"id": "10002", "name": "Incident"},
						"status":    map[string]any{"id": "3", "name": "In Progress"},
						"project":   map[string]any{"id": "10000", "key": "OPS", "name": "Operations"},
					},
				}},
			})
		case "/rest/api/3/issue/OPS-123/changelog":
			writeJSON(t, w, map[string]any{
				"values": []map[string]any{{
					"id":      "20001",
					"created": now.Add(-time.Hour).Format(jiraTimeLayout),
					"items": []map[string]any{{
						"field":      "status",
						"fromString": "To Do",
						"toString":   "In Progress",
					}},
				}},
			})
		case "/rest/api/3/issue/OPS-123/remotelink":
			writeJSON(t, w, []map[string]any{{
				"id":           30001,
				"globalId":     "github:pr:42",
				"relationship": "causes",
				"application":  map[string]any{"name": "GitHub", "type": "com.github"},
				"object":       map[string]any{"url": "https://github.com/example/app/pull/42?token=secret", "title": "PR 42"},
			}})
		default:
			if writeEmptyMetadataResponse(t, w, r) {
				return
			}
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{
		BaseURL: server.URL,
		Email:   "user@example.com",
		Token:   "jira-token",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectWorkItemEvidence(context.Background(), TargetConfig{
		Provider:        ProviderJiraCloud,
		ScopeID:         "jira:site:example",
		SiteID:          "example.atlassian.net",
		IssueLimit:      25,
		ChangelogLimit:  10,
		RemoteLinkLimit: 10,
		JQL:             "project = OPS ORDER BY updated ASC",
	}, CollectionWindow{Since: now.Add(-24 * time.Hour), Until: now})
	if err != nil {
		t.Fatalf("CollectWorkItemEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.Issues), 1; got != want {
		t.Fatalf("len(Issues) = %d, want %d", got, want)
	}
	if got, want := len(result.Transitions["10001"]), 1; got != want {
		t.Fatalf("len(Transitions[10001]) = %d, want %d", got, want)
	}
	if got := result.ExternalLinks["10001"][0].Object.URL; strings.Contains(got, "token=secret") {
		t.Fatalf("external link URL = %q, want sensitive query redacted", got)
	}
	wantPaths := []string{"/rest/api/3/search/jql", "/rest/api/3/issue/OPS-123/changelog", "/rest/api/3/issue/OPS-123/remotelink"}
	for _, want := range wantPaths {
		assertContainsPath(t, requested, want)
	}
}

func TestHTTPClientClassifiesProviderStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorMessages":["hidden"]}`))
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "jira-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	_, err = client.CollectWorkItemEvidence(context.Background(), TargetConfig{IssueLimit: 1}, CollectionWindow{Since: testObservedAt().Add(-time.Hour), Until: testObservedAt()})
	if err == nil {
		t.Fatal("CollectWorkItemEvidence() error = nil, want provider failure")
	}
	var jiraErr JiraError
	if !errors.As(err, &jiraErr) {
		t.Fatalf("CollectWorkItemEvidence() error = %T, want JiraError", err)
	}
	if got, want := jiraErr.StatusCode, http.StatusForbidden; got != want {
		t.Fatalf("StatusCode = %d, want %d", got, want)
	}
}

func TestHTTPClientCollectWorkItemEvidencePaginatesSearchAndChangelog(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	var searchBodies []searchRequest
	var changelogStarts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/search/jql":
			var body searchRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode search body: %v", err)
			}
			searchBodies = append(searchBodies, body)
			assertSearchFieldsAllowlisted(t, body.Fields)
			if body.NextPageToken == "" {
				writeJSON(t, w, map[string]any{
					"isLast":        false,
					"nextPageToken": "page-2",
					"issues": []map[string]any{{
						"id":   "10001",
						"key":  "OPS-123",
						"self": serverURL(r) + "/rest/api/3/issue/10001",
						"fields": map[string]any{
							"summary":   "Checkout alert",
							"created":   now.Add(-2 * time.Hour).Format(jiraTimeLayout),
							"updated":   now.Format(jiraTimeLayout),
							"issuetype": map[string]any{"id": "10002", "name": "Incident"},
							"status":    map[string]any{"id": "3", "name": "In Progress"},
							"project":   map[string]any{"id": "10000", "key": "OPS", "name": "Operations"},
						},
					}},
				})
				return
			}
			if body.NextPageToken != "page-2" {
				t.Fatalf("NextPageToken = %q, want page-2", body.NextPageToken)
			}
			writeJSON(t, w, map[string]any{
				"isLast": true,
				"issues": []map[string]any{{
					"id":   "10002",
					"key":  "OPS-124",
					"self": serverURL(r) + "/rest/api/3/issue/10002",
					"fields": map[string]any{
						"summary":   "Recovery follow-up",
						"created":   now.Add(-time.Hour).Format(jiraTimeLayout),
						"updated":   now.Format(jiraTimeLayout),
						"issuetype": map[string]any{"id": "10002", "name": "Incident"},
						"status":    map[string]any{"id": "10004", "name": "Done"},
						"project":   map[string]any{"id": "10000", "key": "OPS", "name": "Operations"},
					},
				}},
			})
		case "/rest/api/3/issue/OPS-123/changelog":
			startAt := r.URL.Query().Get("startAt")
			changelogStarts = append(changelogStarts, startAt)
			if startAt == "" || startAt == "0" {
				writeJSON(t, w, map[string]any{
					"startAt":    0,
					"maxResults": 1,
					"total":      2,
					"isLast":     false,
					"values": []map[string]any{{
						"id":      "20001",
						"created": now.Add(-time.Hour).Format(jiraTimeLayout),
						"items": []map[string]any{{
							"field":      "status",
							"fromString": "To Do",
							"toString":   "In Progress",
						}},
					}},
				})
				return
			}
			if startAt != "1" {
				t.Fatalf("changelog startAt = %q, want 1", startAt)
			}
			writeJSON(t, w, map[string]any{
				"startAt":    1,
				"maxResults": 1,
				"total":      2,
				"isLast":     true,
				"values": []map[string]any{{
					"id":      "20002",
					"created": now.Add(-30 * time.Minute).Format(jiraTimeLayout),
					"items": []map[string]any{{
						"field":      "status",
						"fromString": "In Progress",
						"toString":   "Done",
					}},
				}},
			})
		case "/rest/api/3/issue/OPS-124/changelog":
			changelogStarts = append(changelogStarts, r.URL.Query().Get("startAt"))
			writeJSON(t, w, map[string]any{"startAt": 0, "maxResults": 1, "total": 0, "isLast": true})
		case "/rest/api/3/issue/OPS-123/remotelink", "/rest/api/3/issue/OPS-124/remotelink":
			writeJSON(t, w, []map[string]any{})
		default:
			if writeEmptyMetadataResponse(t, w, r) {
				return
			}
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "jira-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectWorkItemEvidence(context.Background(), TargetConfig{
		IssueLimit:      2,
		ChangelogLimit:  2,
		RemoteLinkLimit: 2,
	}, CollectionWindow{Since: now.Add(-24 * time.Hour), Until: now})
	if err != nil {
		t.Fatalf("CollectWorkItemEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.Issues), 2; got != want {
		t.Fatalf("len(Issues) = %d, want %d", got, want)
	}
	if got, want := len(result.Transitions["10001"]), 2; got != want {
		t.Fatalf("len(Transitions[10001]) = %d, want %d", got, want)
	}
	if got, want := result.Stats.SearchPages, 2; got != want {
		t.Fatalf("SearchPages = %d, want %d", got, want)
	}
	if got, want := result.Stats.ChangelogPages, 3; got != want {
		t.Fatalf("ChangelogPages = %d, want %d; starts=%v", got, want, changelogStarts)
	}
	if got, want := result.Stats.IssuesEmitted, 2; got != want {
		t.Fatalf("IssuesEmitted = %d, want %d", got, want)
	}
	if got, want := result.Stats.ChangelogEventsEmitted, 2; got != want {
		t.Fatalf("ChangelogEventsEmitted = %d, want %d", got, want)
	}
	if got, want := len(searchBodies), 2; got != want {
		t.Fatalf("search request count = %d, want %d", got, want)
	}
}

func TestHTTPClientRecordsRetryAfterForRateLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.Header().Set("RateLimit-Reason", "jira-burst-based")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"errorMessages":["slow down"]}`))
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "jira-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	_, err = client.CollectWorkItemEvidence(context.Background(), TargetConfig{IssueLimit: 1}, CollectionWindow{Since: testObservedAt().Add(-time.Hour), Until: testObservedAt()})
	if err == nil {
		t.Fatal("CollectWorkItemEvidence() error = nil, want rate limit failure")
	}
	var jiraErr JiraError
	if !errors.As(err, &jiraErr) {
		t.Fatalf("CollectWorkItemEvidence() error = %T, want JiraError", err)
	}
	if got, want := jiraErr.RetryAfter, 7*time.Second; got != want {
		t.Fatalf("RetryAfter = %s, want %s", got, want)
	}
	if got, want := jiraErr.RateLimitReason, "jira-burst-based"; got != want {
		t.Fatalf("RateLimitReason = %q, want %q", got, want)
	}
}

func TestHTTPClientClassifiesMalformedRemoteLinksWithoutFailingCollection(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/search/jql":
			writeJSON(t, w, map[string]any{
				"isLast": true,
				"issues": []map[string]any{{
					"id":   "10001",
					"key":  "OPS-123",
					"self": serverURL(r) + "/rest/api/3/issue/10001",
					"fields": map[string]any{
						"summary":   "Checkout alert",
						"created":   now.Add(-2 * time.Hour).Format(jiraTimeLayout),
						"updated":   now.Format(jiraTimeLayout),
						"issuetype": map[string]any{"id": "10002", "name": "Incident"},
						"status":    map[string]any{"id": "3", "name": "In Progress"},
						"project":   map[string]any{"id": "10000", "key": "OPS", "name": "Operations"},
					},
				}},
			})
		case "/rest/api/3/issue/OPS-123/changelog":
			writeJSON(t, w, map[string]any{"startAt": 0, "maxResults": 1, "total": 0, "isLast": true})
		case "/rest/api/3/issue/OPS-123/remotelink":
			writeJSON(t, w, []map[string]any{
				{"object": map[string]any{"url": "://not a url", "title": "private"}},
				{
					"id":          30001,
					"application": map[string]any{"name": "Unknown Tracker", "type": "unknown"},
					"object":      map[string]any{"url": "https://private.example.invalid/path?token=secret", "title": "private deploy"},
				},
			})
		default:
			if writeEmptyMetadataResponse(t, w, r) {
				return
			}
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "jira-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectWorkItemEvidence(context.Background(), TargetConfig{
		IssueLimit:      1,
		ChangelogLimit:  1,
		RemoteLinkLimit: 10,
	}, CollectionWindow{Since: now.Add(-24 * time.Hour), Until: now})
	if err != nil {
		t.Fatalf("CollectWorkItemEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.ExternalLinks["10001"]), 1; got != want {
		t.Fatalf("len(ExternalLinks[10001]) = %d, want %d", got, want)
	}
	if got, want := result.Stats.RemoteLinksRejected, 1; got != want {
		t.Fatalf("RemoteLinksRejected = %d, want %d", got, want)
	}
	if got, want := result.Stats.RemoteLinksEmitted, 1; got != want {
		t.Fatalf("RemoteLinksEmitted = %d, want %d", got, want)
	}
	if got := result.ExternalLinks["10001"][0].Object.URL; strings.Contains(got, "token=secret") {
		t.Fatalf("remote link URL = %q, want token removed before normalization", got)
	}
}

func TestHTTPClientPartialFailurePreservesBoundedStats(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/search/jql":
			writeJSON(t, w, map[string]any{
				"isLast": true,
				"issues": []map[string]any{{
					"id":   "10001",
					"key":  "OPS-123",
					"self": serverURL(r) + "/rest/api/3/issue/10001",
					"fields": map[string]any{
						"summary":   "Checkout alert",
						"created":   now.Add(-2 * time.Hour).Format(jiraTimeLayout),
						"updated":   now.Format(jiraTimeLayout),
						"issuetype": map[string]any{"id": "10002", "name": "Incident"},
						"status":    map[string]any{"id": "3", "name": "In Progress"},
						"project":   map[string]any{"id": "10000", "key": "OPS", "name": "Operations"},
					},
				}},
			})
		case "/rest/api/3/issue/OPS-123/changelog":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"errorMessages":["temporary changelog failure"]}`))
		default:
			if writeEmptyMetadataResponse(t, w, r) {
				return
			}
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "jira-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	_, err = client.CollectWorkItemEvidence(context.Background(), TargetConfig{
		IssueLimit:      1,
		ChangelogLimit:  1,
		RemoteLinkLimit: 1,
	}, CollectionWindow{Since: now.Add(-24 * time.Hour), Until: now})
	if err == nil {
		t.Fatal("CollectWorkItemEvidence() error = nil, want partial failure")
	}
	var partial PartialCollectionError
	if !errors.As(err, &partial) {
		t.Fatalf("CollectWorkItemEvidence() error = %T, want PartialCollectionError", err)
	}
	if got, want := partial.Stage, "changelog"; got != want {
		t.Fatalf("Stage = %q, want %q", got, want)
	}
	if got, want := partial.Stats.SearchPages, 1; got != want {
		t.Fatalf("SearchPages = %d, want %d", got, want)
	}
	if got, want := partial.Stats.IssuesEmitted, 1; got != want {
		t.Fatalf("IssuesEmitted = %d, want %d", got, want)
	}
	if got, want := partial.Stats.PartialFailures, 1; got != want {
		t.Fatalf("PartialFailures = %d, want %d", got, want)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func writeEmptyMetadataResponse(t *testing.T, w http.ResponseWriter, r *http.Request) bool {
	t.Helper()
	switch r.URL.Path {
	case "/rest/api/3/project/search", "/rest/api/3/statuses/search",
		"/rest/api/3/field/search", "/rest/api/3/workflows/search":
		writeJSON(t, w, map[string]any{"isLast": true, "values": []map[string]any{}})
		return true
	case "/rest/api/3/issuetype/project":
		writeJSON(t, w, []map[string]any{})
		return true
	default:
		return false
	}
}

func testObservedAt() time.Time {
	return time.Date(2026, time.May, 31, 19, 0, 0, 0, time.UTC)
}

func assertSearchFieldsAllowlisted(t *testing.T, fields []string) {
	t.Helper()
	disallowed := map[string]struct{}{
		"attachment":  {},
		"comment":     {},
		"description": {},
	}
	for _, field := range fields {
		if _, ok := disallowed[field]; ok {
			t.Fatalf("search field %q requested; private payload fields must stay out of Jira search", field)
		}
		if strings.HasPrefix(field, "customfield_") {
			t.Fatalf("search field %q requested; raw custom fields are out of scope", field)
		}
	}
}

func serverURL(r *http.Request) string {
	if r.TLS != nil {
		return "https://" + r.Host
	}
	return "http://" + r.Host
}
