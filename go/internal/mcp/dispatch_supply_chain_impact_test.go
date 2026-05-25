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
		"profile":          "comprehensive",
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
	if got, want := route.query["profile"], "comprehensive"; got != want {
		t.Fatalf("route.query[profile] = %#v, want %#v", got, want)
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
					"source_snapshots": []map[string]any{
						{
							"source":                 "first_epss",
							"cache_artifact_version": "vulnerability-source-cache.v1",
							"snapshot_digest":        "sha256:abc",
							"last_updated_at":        "2026-05-24T12:01:00Z",
							"freshness":              "fresh",
							"complete":               true,
						},
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
	snapshots, ok := readiness["source_snapshots"].([]any)
	if !ok {
		t.Fatalf("source_snapshots = %T, want []any", readiness["source_snapshots"])
	}
	if got, want := len(snapshots), 1; got != want {
		t.Fatalf("len(source_snapshots) = %d, want %d", got, want)
	}
}

func TestDispatchToolSupplyChainImpactFindingsSurfacesIncompleteCoverageStates(t *testing.T) {
	t.Parallel()

	// The implementation plan promises MCP tool contract coverage for
	// zero-findings cases with incomplete coverage — i.e., callers must see
	// the server's not_configured / evidence_incomplete / target_incomplete
	// states through the MCP envelope, not just ready_zero_findings.
	cases := []struct {
		name           string
		readinessState string
		missing        []string
	}{
		{
			name:           "not_configured surfaces missing advisory sources",
			readinessState: "not_configured",
			missing:        []string{"advisory_sources"},
		},
		{
			name:           "evidence_incomplete surfaces missing owned packages",
			readinessState: "evidence_incomplete",
			missing:        []string{"owned_packages"},
		},
		{
			name:           "target_incomplete surfaces in-flight collection",
			readinessState: "target_incomplete",
			missing:        []string{"target_collection_incomplete"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/v0/supply-chain/impact/findings", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"findings":  []any{},
						"count":     0,
						"limit":     25,
						"truncated": false,
						"readiness": map[string]any{
							"readiness_state":  tc.readinessState,
							"target_scope":     map[string]any{"repository_id": "repo://example/api"},
							"evidence_sources": []map[string]any{},
							"missing_evidence": tc.missing,
							"freshness":        "unknown",
							"counts":           map[string]any{"findings_returned": 0, "findings_truncated": false, "evidence_facts_total": 0},
						},
					},
					"truth": map[string]any{"level": "exact", "capability": "supply_chain.impact_findings.list", "profile": "production", "basis": "semantic_facts", "freshness": map[string]any{"state": "fresh"}},
					"error": nil,
				})
			})

			result, err := dispatchTool(
				context.Background(),
				mux,
				"list_supply_chain_impact_findings",
				map[string]any{"repository_id": "repo://example/api", "limit": float64(25)},
				"",
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			)
			if err != nil {
				t.Fatalf("dispatchTool() error = %v, want nil", err)
			}
			if result.Envelope == nil {
				t.Fatal("envelope = nil, want incomplete-coverage envelope")
			}
			data := result.Envelope.Data.(map[string]any)
			readiness := data["readiness"].(map[string]any)
			if got := readiness["readiness_state"]; got != tc.readinessState {
				t.Fatalf("readiness_state = %#v, want %#v", got, tc.readinessState)
			}
			missingRaw, ok := readiness["missing_evidence"].([]any)
			if !ok {
				t.Fatalf("missing_evidence = %T, want []any", readiness["missing_evidence"])
			}
			if len(missingRaw) != len(tc.missing) {
				t.Fatalf("len(missing_evidence) = %d, want %d", len(missingRaw), len(tc.missing))
			}
		})
	}
}
