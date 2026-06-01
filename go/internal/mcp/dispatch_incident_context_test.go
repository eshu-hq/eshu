package mcp

import "testing"

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
