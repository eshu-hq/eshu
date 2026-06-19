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

type fakeReportSupplyChainSource struct {
	gotWorkload string
}

func (f *fakeReportSupplyChainSource) SupplyChainInventoryForWorkload(_ context.Context, workloadID string) (map[string]any, error) {
	f.gotWorkload = workloadID
	return map[string]any{"count": 2, "truncated": false}, nil
}

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
	supplyChain := &fakeReportSupplyChainSource{}
	(&serviceintelhttp.ReportHandler{Entities: entities, SupplyChain: supplyChain}).Mount(mux)

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
	if supplyChain.gotWorkload != "workload:sample-service-api" {
		t.Fatalf("supply-chain source workload = %q, want workload:sample-service-api", supplyChain.gotWorkload)
	}
	supply := reportSectionByKind(t, sections, "supply_chain")
	if supply["status"] == "unsupported" {
		t.Fatalf("sourced supply_chain section status = unsupported, want sourced")
	}
	packet, ok := supply["answer"].(map[string]any)
	if !ok {
		t.Fatalf("supply_chain answer = %#v", supply["answer"])
	}
	truth, ok := packet["truth"].(map[string]any)
	if !ok {
		t.Fatalf("supply_chain truth = %#v", packet["truth"])
	}
	if got, want := truth["capability"], "supply_chain.impact_findings.aggregate"; got != want {
		t.Fatalf("supply_chain truth capability = %v, want %s", got, want)
	}
}

func reportSectionByKind(t *testing.T, sections []any, kind string) map[string]any {
	t.Helper()
	for _, section := range sections {
		row, ok := section.(map[string]any)
		if ok && row["kind"] == kind {
			return row
		}
	}
	t.Fatalf("section %q not present in %#v", kind, sections)
	return nil
}
