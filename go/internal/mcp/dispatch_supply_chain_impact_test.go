package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
)

func TestResolveRouteMapsSupplyChainImpactFindingsToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_supply_chain_impact_findings", map[string]any{
		"after_finding_id": "finding-1",
		"cve_id":           "CVE-2026-0001",
		"impact_status":    "affected_exact",
		"limit":            float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/impact/findings"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["cve_id"], "CVE-2026-0001"; got != want {
		t.Fatalf("route.query[cve_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["impact_status"], "affected_exact"; got != want {
		t.Fatalf("route.query[impact_status] = %#v, want %#v", got, want)
	}
	if got, want := route.query["limit"], "25"; got != want {
		t.Fatalf("route.query[limit] = %#v, want %#v", got, want)
	}
}

func TestDispatchToolSupplyChainImpactFindingsReturnsReadinessEnvelope(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/supply-chain/impact/findings", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), "application/eshu.envelope+json"; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("repository_id"), "repo://example/api"; got != want {
			t.Fatalf("repository_id = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"findings":  []any{},
				"count":     0,
				"limit":     25,
				"truncated": false,
				"readiness": map[string]any{
					"readiness_state": "ready_zero_findings",
					"target_scope":    map[string]any{"repository_id": "repo://example/api"},
					"evidence_sources": []map[string]any{
						{"family": "vulnerability.advisory", "fact_count": 5, "freshness": "fresh"},
						{"family": "package.consumption", "fact_count": 2, "freshness": "fresh"},
					},
					"freshness": "fresh",
					"counts": map[string]any{
						"findings_returned":    0,
						"findings_truncated":   false,
						"evidence_facts_total": 7,
					},
				},
			},
			"truth": map[string]any{
				"level":      "exact",
				"capability": "supply_chain.impact_findings.list",
				"profile":    "production",
				"basis":      "semantic_facts",
				"freshness":  map[string]any{"state": "fresh"},
			},
			"error": nil,
		})
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"list_supply_chain_impact_findings",
		map[string]any{
			"repository_id": "repo://example/api",
			"limit":         float64(25),
		},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want supply-chain impact envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %T, want map[string]any", result.Envelope.Data)
	}
	readiness, ok := data["readiness"].(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data[readiness] = %T, want map[string]any", data["readiness"])
	}
	if got, want := readiness["readiness_state"], "ready_zero_findings"; got != want {
		t.Fatalf("readiness_state = %#v, want %#v", got, want)
	}
	if got, want := readiness["freshness"], "fresh"; got != want {
		t.Fatalf("freshness = %#v, want %#v", got, want)
	}
	sources, ok := readiness["evidence_sources"].([]any)
	if !ok {
		t.Fatalf("evidence_sources = %T, want []any", readiness["evidence_sources"])
	}
	if got, want := len(sources), 2; got != want {
		t.Fatalf("len(evidence_sources) = %d, want %d", got, want)
	}
}
