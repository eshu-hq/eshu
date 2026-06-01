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
	for i, want := range wantPaths {
		if requested[i] != want {
			t.Fatalf("requested[%d] = %q, want %q; all %#v", i, requested[i], want, requested)
		}
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

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func testObservedAt() time.Time {
	return time.Date(2026, time.May, 31, 19, 0, 0, 0, time.UTC)
}
