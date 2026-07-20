// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestHTTPClientFollowsPaginatedIncidentPagesUntilMoreIsFalse(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	var incidentOffsets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/incidents":
			incidentOffsets = append(incidentOffsets, r.URL.Query().Get("offset"))
			if len(incidentOffsets) == 1 {
				writeJSON(t, w, map[string]any{
					"more": true,
					"incidents": []map[string]any{
						{"id": "P1", "created_at": now.Format(time.RFC3339)},
						{"id": "P2", "created_at": now.Format(time.RFC3339)},
					},
				})
				return
			}
			writeJSON(t, w, map[string]any{
				"more": false,
				"incidents": []map[string]any{
					{"id": "P3", "created_at": now.Format(time.RFC3339)},
				},
			})
		case "/incidents/P1/log_entries", "/incidents/P2/log_entries", "/incidents/P3/log_entries":
			writeJSON(t, w, map[string]any{"log_entries": []map[string]any{}})
		case "/incidents/P1/related_change_events", "/incidents/P2/related_change_events", "/incidents/P3/related_change_events":
			writeJSON(t, w, map[string]any{"change_events": []map[string]any{}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectIncidentEvidence(context.Background(), TargetConfig{
		Provider:  ProviderPagerDuty,
		ScopeID:   "pagerduty:account:example",
		AccountID: "example",
		Token:     "pd-token",
	}, CollectionWindow{Since: now.Add(-time.Hour), Until: now})
	if err != nil {
		t.Fatalf("CollectIncidentEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.Incidents), 3; got != want {
		t.Fatalf("len(Incidents) = %d, want %d (previously the collector stopped at page 1)", got, want)
	}
	if got, want := len(incidentOffsets), 2; got != want {
		t.Fatalf("incident page requests = %d, want %d", got, want)
	}
	if got, want := incidentOffsets[0], ""; got != want {
		t.Fatalf("first page offset = %q, want %q (omitted)", got, want)
	}
	if got, want := incidentOffsets[1], "2"; got != want {
		t.Fatalf("second page offset = %q, want %q", got, want)
	}
	if result.Truncated {
		t.Fatal("Truncated = true, want false: pagination exhausted more naturally")
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %#v, want none when more naturally exhausts", result.Warnings)
	}
	// PagesFetched is the total across every paginated resource: 2 incident
	// pages, plus one log-entry page and one related-change-event page per
	// each of the 3 incidents (2 + 3*2 = 8).
	if got, want := result.PagesFetched, 8; got != want {
		t.Fatalf("PagesFetched = %d, want %d", got, want)
	}
}

func TestHTTPClientCollectIncidentEvidenceSinglePageOmitsTruncation(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/incidents":
			writeJSON(t, w, map[string]any{
				"more":      false,
				"incidents": []map[string]any{{"id": "P1", "created_at": now.Format(time.RFC3339)}},
			})
		case "/incidents/P1/log_entries":
			writeJSON(t, w, map[string]any{"log_entries": []map[string]any{}})
		case "/incidents/P1/related_change_events":
			writeJSON(t, w, map[string]any{"change_events": []map[string]any{}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectIncidentEvidence(context.Background(), TargetConfig{
		Provider:  ProviderPagerDuty,
		ScopeID:   "pagerduty:account:example",
		AccountID: "example",
		Token:     "pd-token",
	}, CollectionWindow{Since: now.Add(-time.Hour), Until: now})
	if err != nil {
		t.Fatalf("CollectIncidentEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.Incidents), 1; got != want {
		t.Fatalf("len(Incidents) = %d, want %d", got, want)
	}
	if result.Truncated {
		t.Fatal("Truncated = true, want false for a single exhausted page")
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %#v, want none", result.Warnings)
	}
	// One incident page, one log-entry page, one related-change-event page.
	if got, want := result.PagesFetched, 3; got != want {
		t.Fatalf("PagesFetched = %d, want %d", got, want)
	}
}

func TestHTTPClientCollectIncidentEvidenceSetsTruncatedWhenPageBoundHit(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	pageCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/incidents":
			pageCalls++
			id := "P" + strconv.Itoa(pageCalls)
			writeJSON(t, w, map[string]any{
				"more":      true,
				"incidents": []map[string]any{{"id": id, "created_at": now.Format(time.RFC3339)}},
			})
		case "/incidents/P1/log_entries", "/incidents/P2/log_entries":
			writeJSON(t, w, map[string]any{"log_entries": []map[string]any{}})
		case "/incidents/P1/related_change_events", "/incidents/P2/related_change_events":
			writeJSON(t, w, map[string]any{"change_events": []map[string]any{}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectIncidentEvidence(context.Background(), TargetConfig{
		Provider:           ProviderPagerDuty,
		ScopeID:            "pagerduty:account:example",
		AccountID:          "example",
		Token:              "pd-token",
		PaginationMaxPages: 2,
	}, CollectionWindow{Since: now.Add(-time.Hour), Until: now})
	if err != nil {
		t.Fatalf("CollectIncidentEvidence() error = %v, want nil", err)
	}
	if got, want := pageCalls, 2; got != want {
		t.Fatalf("incident page requests = %d, want exactly %d (bounded)", got, want)
	}
	if got, want := len(result.Incidents), 2; got != want {
		t.Fatalf("len(Incidents) = %d, want %d", got, want)
	}
	if !result.Truncated {
		t.Fatal("Truncated = false, want true when the max-page bound is hit while more pages remain")
	}
	if got, want := len(result.Warnings), 1; got != want {
		t.Fatalf("len(Warnings) = %d, want %d", got, want)
	}
	if got, want := result.Warnings[0].ResourceClass, ConfigResourceClassIncident; got != want {
		t.Fatalf("warning ResourceClass = %q, want %q", got, want)
	}
	if got, want := result.Warnings[0].Reason, ConfigWarningTruncated; got != want {
		t.Fatalf("warning Reason = %q, want %q", got, want)
	}
}

func TestHTTPClientFollowsPaginatedLogEntryPages(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	var logEntryOffsets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/incidents":
			writeJSON(t, w, map[string]any{
				"more":      false,
				"incidents": []map[string]any{{"id": "P1", "created_at": now.Format(time.RFC3339)}},
			})
		case "/incidents/P1/log_entries":
			logEntryOffsets = append(logEntryOffsets, r.URL.Query().Get("offset"))
			if len(logEntryOffsets) == 1 {
				writeJSON(t, w, map[string]any{
					"more": true,
					"log_entries": []map[string]any{
						{"id": "R1", "created_at": now.Format(time.RFC3339)},
					},
				})
				return
			}
			writeJSON(t, w, map[string]any{
				"more": false,
				"log_entries": []map[string]any{
					{"id": "R2", "created_at": now.Format(time.RFC3339)},
				},
			})
		case "/incidents/P1/related_change_events":
			writeJSON(t, w, map[string]any{"change_events": []map[string]any{}})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectIncidentEvidence(context.Background(), TargetConfig{
		Provider:  ProviderPagerDuty,
		ScopeID:   "pagerduty:account:example",
		AccountID: "example",
		Token:     "pd-token",
	}, CollectionWindow{Since: now.Add(-time.Hour), Until: now})
	if err != nil {
		t.Fatalf("CollectIncidentEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.LifecycleEvents["P1"]), 2; got != want {
		t.Fatalf("len(LifecycleEvents[P1]) = %d, want %d (previously the collector stopped at page 1)", got, want)
	}
	if got, want := len(logEntryOffsets), 2; got != want {
		t.Fatalf("log-entry page requests = %d, want %d", got, want)
	}
}

func TestHTTPClientFollowsPaginatedRelatedChangeEventPages(t *testing.T) {
	t.Parallel()

	now := testObservedAt()
	var changeEventOffsets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/incidents":
			writeJSON(t, w, map[string]any{
				"more":      false,
				"incidents": []map[string]any{{"id": "P1", "created_at": now.Format(time.RFC3339)}},
			})
		case "/incidents/P1/log_entries":
			writeJSON(t, w, map[string]any{"log_entries": []map[string]any{}})
		case "/incidents/P1/related_change_events":
			changeEventOffsets = append(changeEventOffsets, r.URL.Query().Get("offset"))
			if len(changeEventOffsets) == 1 {
				writeJSON(t, w, map[string]any{
					"more": true,
					"change_events": []map[string]any{
						{"id": "CE1", "timestamp": now.Format(time.RFC3339)},
					},
				})
				return
			}
			writeJSON(t, w, map[string]any{
				"more": false,
				"change_events": []map[string]any{
					{"id": "CE2", "timestamp": now.Format(time.RFC3339)},
				},
			})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result, err := client.CollectIncidentEvidence(context.Background(), TargetConfig{
		Provider:  ProviderPagerDuty,
		ScopeID:   "pagerduty:account:example",
		AccountID: "example",
		Token:     "pd-token",
	}, CollectionWindow{Since: now.Add(-time.Hour), Until: now})
	if err != nil {
		t.Fatalf("CollectIncidentEvidence() error = %v, want nil", err)
	}
	if got, want := len(result.RelatedChangeEvents["P1"]), 2; got != want {
		t.Fatalf("len(RelatedChangeEvents[P1]) = %d, want %d (previously the collector stopped at page 1)", got, want)
	}
	if got, want := len(changeEventOffsets), 2; got != want {
		t.Fatalf("related-change-event page requests = %d, want %d", got, want)
	}
}
