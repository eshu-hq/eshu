// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBoundedJQLAddsReplayableUpdatedWindow(t *testing.T) {
	t.Parallel()

	window := CollectionWindow{
		Since: time.Date(2026, time.May, 31, 17, 0, 0, 0, time.UTC),
		Until: time.Date(2026, time.May, 31, 18, 0, 0, 0, time.UTC),
	}
	got := boundedJQL("project = OPS ORDER BY priority DESC", window)
	want := `(project = OPS) AND updated >= "2026-05-31 17:00" AND updated <= "2026-05-31 18:00" ORDER BY priority DESC`
	if got != want {
		t.Fatalf("boundedJQL() = %q, want %q", got, want)
	}
}

func TestHTTPClientClassifiesVisibilityAndArchiveStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   FailureClass
	}{
		{name: "permission hidden", status: http.StatusForbidden, want: FailurePermissionHidden},
		{name: "deleted or hidden by absence", status: http.StatusNotFound, want: FailureDeleted},
		{name: "archived", status: http.StatusGone, want: FailureArchived},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"errorMessages":["bounded provider status"]}`))
			}))
			defer server.Close()

			client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "jira-token", Client: server.Client()})
			if err != nil {
				t.Fatalf("NewHTTPClient() error = %v, want nil", err)
			}
			_, err = client.CollectWorkItemEvidence(context.Background(), TargetConfig{IssueLimit: 1}, CollectionWindow{
				Since: testObservedAt().Add(-time.Hour),
				Until: testObservedAt(),
			})
			if err == nil {
				t.Fatal("CollectWorkItemEvidence() error = nil, want provider failure")
			}
			got := classifiedProviderFailure(err)
			if got.FailureClass() != tt.want {
				t.Fatalf("FailureClass = %q, want %q", got.FailureClass(), tt.want)
			}
			if tt.status != http.StatusGone {
				var jiraErr JiraError
				if !errors.As(err, &jiraErr) {
					t.Fatalf("error = %T, want JiraError", err)
				}
				if strings.Contains(jiraErr.Error(), "jira-token") {
					t.Fatalf("JiraError leaked token: %q", jiraErr.Error())
				}
			}
		})
	}
}
