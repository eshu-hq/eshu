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

func TestHTTPClientCollectsMetadataDefinitionsAndWarnings(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/rest/api/3/search/jql":
			writeJSON(t, w, map[string]any{"isLast": true, "issues": []map[string]any{}})
		case "/rest/api/3/project/search":
			writeJSON(t, w, map[string]any{
				"isLast": true,
				"values": []map[string]any{{
					"id": "10000", "key": "OPS", "name": "Private Operations", "self": serverURL(r) + "/rest/api/3/project/OPS?token=secret",
					"projectTypeKey": "software", "style": "classic", "archived": true,
					"projectCategory": map[string]any{"id": "20000", "name": "Regulated Customers"},
					"insight":         map[string]any{"lastIssueUpdateTime": now.Format(jiraTimeLayout), "totalIssueCount": 12},
				}},
			})
		case "/rest/api/3/issuetype/project":
			if got := r.URL.Query().Get("projectId"); got != "10000" {
				t.Fatalf("issue type projectId = %q, want 10000", got)
			}
			writeJSON(t, w, []map[string]any{{
				"id": "10002", "name": "Incident", "description": "private issue type description",
				"hierarchyLevel": 0, "subtask": false, "scope": map[string]any{"type": "PROJECT", "project": map[string]any{"id": "10000"}},
			}})
		case "/rest/api/3/statuses/search":
			writeJSON(t, w, map[string]any{
				"isLast": true,
				"values": []map[string]any{{
					"id": "3", "name": "In Progress", "description": "private status description",
					"statusCategory": "IN_PROGRESS", "scope": map[string]any{"type": "PROJECT", "project": map[string]any{"id": "10000"}},
				}},
			})
		case "/rest/api/3/field/search":
			writeJSON(t, w, map[string]any{
				"isLast": true,
				"values": []map[string]any{{
					"id": "customfield_10042", "name": "Customer account owner", "description": "owner@example.com",
					"schema": map[string]any{"type": "array", "items": "user", "custom": "com.atlassian.jira.plugin.system.customfieldtypes:multiuserpicker", "customId": 10042},
				}},
			})
		case "/rest/api/3/workflows/search":
			writeJSON(t, w, map[string]any{
				"isLast": false,
				"values": []map[string]any{{
					"id": "workflow-1", "name": "Customer Incident Workflow", "scope": map[string]any{"type": "PROJECT", "project": map[string]any{"id": "10000"}},
					"version":  map[string]any{"id": "version-1", "versionNumber": 3},
					"statuses": []map[string]any{{"statusReference": "todo-ref"}, {"statusReference": "done-ref"}},
					"transitions": []map[string]any{{
						"id": "41", "name": "Resolve customer issue", "type": "DIRECTED", "toStatusReference": "done-ref",
						"links":      []map[string]any{{"fromStatusReference": "todo-ref"}},
						"validators": []map[string]any{{"type": "permission"}},
					}},
				}},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "jira-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectWorkItemEvidence(context.Background(), TargetConfig{
		Provider:      ProviderJiraCloud,
		ScopeID:       "jira:site:example",
		SiteID:        "example.atlassian.net",
		IssueLimit:    1,
		MetadataLimit: 10,
	}, CollectionWindow{Since: now.Add(-24 * time.Hour), Until: now})
	if err != nil {
		t.Fatalf("CollectWorkItemEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.Projects), 1; got != want {
		t.Fatalf("len(Projects) = %d, want %d", got, want)
	}
	if got, want := len(result.IssueTypes), 1; got != want {
		t.Fatalf("len(IssueTypes) = %d, want %d", got, want)
	}
	if got, want := len(result.Statuses), 1; got != want {
		t.Fatalf("len(Statuses) = %d, want %d", got, want)
	}
	if got, want := len(result.Fields), 1; got != want {
		t.Fatalf("len(Fields) = %d, want %d", got, want)
	}
	if got, want := len(result.Workflows), 1; got != want {
		t.Fatalf("len(Workflows) = %d, want %d", got, want)
	}
	if result.Stats.MetadataObjectsScanned == 0 || result.Stats.MetadataObjectsEmitted == 0 {
		t.Fatalf("metadata stats = %#v, want scanned and emitted counts", result.Stats)
	}
	if result.Stats.MetadataRedactions == 0 {
		t.Fatalf("MetadataRedactions = 0, want private metadata redactions counted")
	}
	assertContainsPath(t, requested, "/rest/api/3/project/search")
	assertContainsPath(t, requested, "/rest/api/3/issuetype/project")
	assertContainsPath(t, requested, "/rest/api/3/statuses/search")
	assertContainsPath(t, requested, "/rest/api/3/field/search")
	assertContainsPath(t, requested, "/rest/api/3/workflows/search")
}

func TestHTTPClientRecordsPermissionHiddenMetadataWithoutFailingCollection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/search/jql":
			writeJSON(t, w, map[string]any{"isLast": true, "issues": []map[string]any{}})
		case "/rest/api/3/project/search", "/rest/api/3/statuses/search", "/rest/api/3/field/search":
			writeJSON(t, w, map[string]any{"isLast": true, "values": []map[string]any{}})
		case "/rest/api/3/workflows/search":
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"errorMessages": []string{"hidden"}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "jira-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectWorkItemEvidence(context.Background(), TargetConfig{MetadataLimit: 10}, CollectionWindow{Since: now.Add(-time.Hour), Until: now})
	if err != nil {
		t.Fatalf("CollectWorkItemEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.MetadataWarnings), 1; got != want {
		t.Fatalf("len(MetadataWarnings) = %d, want %d", got, want)
	}
	if got := result.MetadataWarnings[0].Reason; got != "permission_hidden" {
		t.Fatalf("warning reason = %q, want permission_hidden", got)
	}
	if got := result.Stats.PermissionHiddenMetadata; got != 1 {
		t.Fatalf("PermissionHiddenMetadata = %d, want 1", got)
	}
}

func assertContainsPath(t *testing.T, got []string, want string) {
	t.Helper()
	for _, path := range got {
		if path == want {
			return
		}
	}
	t.Fatalf("requested paths %#v missing %q", got, want)
}
