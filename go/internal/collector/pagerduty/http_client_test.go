// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

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

func TestHTTPClientCollectIncidentEvidenceUsesBoundedPagerDutyEndpoints(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Token token=pd-token"; got != want {
			t.Fatalf("Authorization = %q, want %q", got, want)
		}
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/incidents":
			if got := r.URL.Query().Get("since"); got == "" {
				t.Fatal("incidents request missing since")
			}
			if got := r.URL.Query().Get("until"); got == "" {
				t.Fatal("incidents request missing until")
			}
			if got, want := r.URL.Query().Get("limit"), "50"; got != want {
				t.Fatalf("incident limit = %q, want %q", got, want)
			}
			writeJSON(t, w, map[string]any{
				"incidents": []map[string]any{{
					"id":              "P123",
					"incident_number": 123,
					"title":           "checkout-api latency",
					"status":          "triggered",
					"urgency":         "high",
					"service":         map[string]any{"id": "SVC1", "summary": "checkout-api", "html_url": "https://example.pagerduty.com/services/SVC1"},
					"assignments":     []map[string]any{{"assignee": map[string]any{"id": "USR1", "summary": "primary oncall"}}},
					"created_at":      now.Add(-15 * time.Minute).Format(time.RFC3339),
					"updated_at":      now.Format(time.RFC3339),
					"html_url":        "https://example.pagerduty.com/incidents/P123",
				}},
			})
		case "/incidents/P123/log_entries":
			writeJSON(t, w, map[string]any{
				"log_entries": []map[string]any{{
					"id":         "R1",
					"type":       "acknowledge_log_entry",
					"summary":    "Acknowledged",
					"created_at": now.Add(-4 * time.Minute).Format(time.RFC3339),
					"agent":      map[string]any{"id": "USR1", "summary": "primary oncall"},
					"channel":    map[string]any{"type": "web"},
					"html_url":   "https://example.pagerduty.com/incidents/P123/log_entries/R1",
				}},
			})
		case "/incidents/P123/related_change_events":
			writeJSON(t, w, map[string]any{
				"change_events": []map[string]any{{
					"id":        "CE1",
					"summary":   "Deploy checkout-api",
					"source":    "github",
					"timestamp": now.Add(-20 * time.Minute).Format(time.RFC3339),
					"html_url":  "https://example.pagerduty.com/change_events/CE1",
					"links":     []map[string]any{{"href": "https://github.com/example/checkout-api/pull/42?token=secret", "text": "PR 42"}},
				}},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{
		BaseURL: server.URL,
		Token:   "pd-token",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectIncidentEvidence(context.Background(), TargetConfig{
		Provider:          ProviderPagerDuty,
		ScopeID:           "pagerduty:account:example",
		AccountID:         "example",
		Token:             "pd-token",
		IncidentLimit:     50,
		LogEntryLimit:     25,
		ChangeEventLimit:  25,
		AllowedServiceIDs: []string{"SVC1"},
	}, CollectionWindow{Since: now.Add(-time.Hour), Until: now})
	if err != nil {
		t.Fatalf("CollectIncidentEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.Incidents), 1; got != want {
		t.Fatalf("len(Incidents) = %d, want %d", got, want)
	}
	if got, want := result.Incidents[0].Service.ID, "SVC1"; got != want {
		t.Fatalf("incident service id = %q, want %q", got, want)
	}
	if got, want := len(result.LifecycleEvents["P123"]), 1; got != want {
		t.Fatalf("len(LifecycleEvents[P123]) = %d, want %d", got, want)
	}
	if got, want := len(result.RelatedChangeEvents["P123"]), 1; got != want {
		t.Fatalf("len(RelatedChangeEvents[P123]) = %d, want %d", got, want)
	}
	if strings.Contains(result.RelatedChangeEvents["P123"][0].Links[0].Href, "token=secret") {
		t.Fatalf("related change link = %q, want sensitive query redacted", result.RelatedChangeEvents["P123"][0].Links[0].Href)
	}
	wantPaths := []string{"/incidents", "/incidents/P123/log_entries", "/incidents/P123/related_change_events"}
	for i, want := range wantPaths {
		if requested[i] != want {
			t.Fatalf("requested[%d] = %q, want %q; all paths %#v", i, requested[i], want, requested)
		}
	}
}

func TestHTTPClientKeepsIncidentEvidenceWhenRelatedChangeEventsArePermissionHidden(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/incidents":
			writeJSON(t, w, map[string]any{
				"incidents": []map[string]any{{
					"id":              "P123",
					"incident_number": 123,
					"title":           "checkout-api latency",
					"status":          "triggered",
					"created_at":      now.Add(-15 * time.Minute).Format(time.RFC3339),
					"updated_at":      now.Format(time.RFC3339),
				}},
			})
		case "/incidents/P123/log_entries":
			writeJSON(t, w, map[string]any{
				"log_entries": []map[string]any{{
					"id":         "R1",
					"type":       "acknowledge_log_entry",
					"created_at": now.Add(-4 * time.Minute).Format(time.RFC3339),
				}},
			})
		case "/incidents/P123/related_change_events":
			w.WriteHeader(http.StatusForbidden)
			writeJSON(t, w, map[string]any{"error": map[string]any{"message": "permission denied"}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{
		BaseURL: server.URL,
		Token:   "pd-token",
		Client:  server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}

	result, err := client.CollectIncidentEvidence(context.Background(), TargetConfig{
		Provider:         ProviderPagerDuty,
		ScopeID:          "pagerduty:account:example",
		AccountID:        "example",
		Token:            "pd-token",
		IncidentLimit:    1,
		LogEntryLimit:    1,
		ChangeEventLimit: 1,
	}, CollectionWindow{Since: now.Add(-time.Hour), Until: now})
	if err != nil {
		t.Fatalf("CollectIncidentEvidence() error = %v, want nil partial result", err)
	}
	if got, want := len(result.Incidents), 1; got != want {
		t.Fatalf("len(Incidents) = %d, want %d", got, want)
	}
	if got, want := len(result.LifecycleEvents["P123"]), 1; got != want {
		t.Fatalf("len(LifecycleEvents[P123]) = %d, want %d", got, want)
	}
	if got, want := len(result.RelatedChangeEvents["P123"]), 0; got != want {
		t.Fatalf("len(RelatedChangeEvents[P123]) = %d, want %d", got, want)
	}
	if got, want := len(result.Warnings), 1; got != want {
		t.Fatalf("len(Warnings) = %d, want %d", got, want)
	}
	if got, want := result.Warnings[0].ResourceClass, ConfigResourceClassRelatedChangeEvent; got != want {
		t.Fatalf("warning ResourceClass = %q, want %q", got, want)
	}
	if got, want := result.Warnings[0].Reason, ConfigWarningPermissionHidden; got != want {
		t.Fatalf("warning Reason = %q, want %q", got, want)
	}
}

func TestHTTPClientClassifiesProviderStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"token pd-token hit limit"}}`))
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	_, err = client.CollectIncidentEvidence(context.Background(), TargetConfig{IncidentLimit: 1}, CollectionWindow{Since: testObservedAt().Add(-time.Hour), Until: testObservedAt()})
	if err == nil {
		t.Fatal("CollectIncidentEvidence() error = nil, want provider failure")
	}
	var pdErr PagerDutyError
	if !errors.As(err, &pdErr) {
		t.Fatalf("CollectIncidentEvidence() error = %T, want PagerDutyError", err)
	}
	if got, want := pdErr.StatusCode, http.StatusTooManyRequests; got != want {
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
