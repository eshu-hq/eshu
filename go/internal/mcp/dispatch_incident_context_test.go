// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestResolveRouteMapsIncidentContextToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_incident_context", map[string]any{
		"provider":             "pagerduty",
		"provider_incident_id": "PABC123",
		"scope_id":             "pagerduty-prod",
		"service_id":           "P-SVC",
		"since":                "2026-05-31T11:00:00Z",
		"until":                "2026-05-31T13:00:00Z",
		"limit":                float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/incidents/PABC123/context"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"provider":   "pagerduty",
		"scope_id":   "pagerduty-prod",
		"service_id": "P-SVC",
		"since":      "2026-05-31T11:00:00Z",
		"until":      "2026-05-31T13:00:00Z",
		"limit":      "25",
	} {
		if got := route.query[key]; got != want {
			t.Fatalf("route.query[%s] = %q, want %q", key, got, want)
		}
	}
}

func TestDispatchToolIncidentContextReturnsStructuredEnvelopeData(t *testing.T) {
	t.Parallel()

	store := fakeIncidentContextStore{
		snapshot: query.IncidentContextSnapshot{
			Query: query.IncidentContextQuery{
				Provider:           "pagerduty",
				ProviderIncidentID: "PABC123",
				Limit:              6,
			},
			Incident: query.IncidentContextIncident{
				Provider:           "pagerduty",
				ProviderIncidentID: "PABC123",
				ScopeID:            "pagerduty:account:prod",
				Title:              "checkout-api elevated errors",
				EvidenceFactID:     "incident-fact",
				SourceConfidence:   "reported",
			},
		},
	}
	mux := http.NewServeMux()
	handler := &query.IncidentHandler{
		Context: &store,
		Profile: query.ProfileProduction,
	}
	handler.Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"get_incident_context",
		map[string]any{
			"provider":             "pagerduty",
			"provider_incident_id": "PABC123",
			"limit":                float64(5),
		},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want structured incident context envelope")
	}
	if result.IsError {
		t.Fatal("dispatchTool() IsError = true, want false")
	}
	if result.Envelope.Truth == nil {
		t.Fatal("dispatchTool() envelope truth is nil, want incident context truth")
	}
	if got, want := result.Envelope.Truth.Capability, "incident.context.read"; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
	if got, want := store.filter.Limit, 6; got != want {
		t.Fatalf("store filter limit = %d, want %d", got, want)
	}

	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", result.Envelope.Data)
	}
	incident, ok := data["incident"].(map[string]any)
	if !ok {
		t.Fatalf("incident type = %T, want map[string]any", data["incident"])
	}
	if got, want := incident["provider_incident_id"], "PABC123"; got != want {
		t.Fatalf("incident.provider_incident_id = %#v, want %#v", got, want)
	}
	missing, ok := data["missing_evidence"].([]any)
	if !ok {
		t.Fatalf("missing_evidence type = %T, want []any", data["missing_evidence"])
	}
	if !incidentContextMissingSlot(missing, "service") || !incidentContextMissingSlot(missing, "work_item") {
		t.Fatalf("missing_evidence = %#v, want service and work_item slots", missing)
	}
}

type fakeIncidentContextStore struct {
	snapshot query.IncidentContextSnapshot
	err      error
	filter   query.IncidentContextFilter
}

func (s *fakeIncidentContextStore) ReadIncidentContext(
	_ context.Context,
	filter query.IncidentContextFilter,
) (query.IncidentContextSnapshot, error) {
	s.filter = filter
	return s.snapshot, s.err
}

func incidentContextMissingSlot(missing []any, slot string) bool {
	for _, item := range missing {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if entry["slot"] == slot {
			return true
		}
	}
	return false
}
