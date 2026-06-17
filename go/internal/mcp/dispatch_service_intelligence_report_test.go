package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/serviceintelhttp"
)

func TestResolveRouteServiceIntelligenceReport(t *testing.T) {
	t.Parallel()
	route, err := resolveRoute("get_service_intelligence_report", map[string]any{
		"workload_id": "workload:sample-service-api",
		"environment": "prod",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v", err)
	}
	if route.method != "GET" {
		t.Fatalf("method = %q, want GET", route.method)
	}
	if route.path != "/api/v0/services/sample-service-api/intelligence-report" {
		t.Fatalf("path = %q", route.path)
	}
	if route.query["environment"] != "prod" {
		t.Fatalf("environment query lost: %#v", route.query)
	}
	if route.query["service_id"] != "workload:sample-service-api" {
		t.Fatalf("service_id query = %q, want workload:sample-service-api", route.query["service_id"])
	}
}

func TestResolveRouteServiceIntelligenceReportRequiresSelector(t *testing.T) {
	t.Parallel()
	if _, err := resolveRoute("get_service_intelligence_report", map[string]any{}); err == nil {
		t.Fatal("expected error when no workload_id/service_name provided")
	}
}

// TestDispatchServiceIntelligenceReportParity proves the MCP tool dispatches
// through the real report HTTP handler to a composed report: the same route the
// API serves, so API and MCP return the same artifact.
func TestDispatchServiceIntelligenceReportParity(t *testing.T) {
	t.Parallel()

	entities := &query.EntityHandler{
		Neo4j:   mcpServiceStorySpecCountGraphReader{t: t},
		Content: mcpNoopContentStore{},
		Profile: query.ProfileProduction,
	}
	mux := http.NewServeMux()
	entities.Mount(mux)
	(&serviceintelhttp.ReportHandler{Entities: entities}).Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"get_service_intelligence_report",
		map[string]any{"workload_id": "workload:sample-service-api"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v", err)
	}
	if result.Envelope == nil {
		t.Fatal("envelope is nil, want composed report")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map[string]any", result.Envelope.Data)
	}
	if got := data["schema"]; got != "service_intelligence_report.v1" {
		t.Fatalf("schema = %v, want service_intelligence_report.v1", got)
	}
	if got := data["supported"]; got != true {
		t.Fatalf("supported = %v, want true", got)
	}
	sections, ok := data["sections"].([]any)
	if !ok || len(sections) != 5 {
		t.Fatalf("sections = %#v, want 5", data["sections"])
	}
	identity, ok := sections[0].(map[string]any)
	if !ok || identity["kind"] != "identity" {
		t.Fatalf("first section = %#v, want identity", sections[0])
	}
	if identity["status"] != "supported" {
		t.Fatalf("identity status = %v, want supported", identity["status"])
	}
}
