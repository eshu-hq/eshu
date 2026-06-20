package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
)

func TestResolveRouteMapsSupplyChainImpactExplainToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("explain_supply_chain_impact", map[string]any{
		"advisory_id":    "GHSA-test",
		"package_id":     "pkg:npm/left-pad",
		"repository_id":  "repo://example/api",
		"subject_digest": "sha256:abc",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/impact/explain"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["advisory_id"], "GHSA-test"; got != want {
		t.Fatalf("route.query[advisory_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["package_id"], "pkg:npm/left-pad"; got != want {
		t.Fatalf("route.query[package_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["repository_id"], "repo://example/api"; got != want {
		t.Fatalf("route.query[repository_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["subject_digest"], "sha256:abc"; got != want {
		t.Fatalf("route.query[subject_digest] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsSupplyChainImpactExplainOperationalAnchors(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("explain_supply_chain_impact", map[string]any{
		"cve_id":      "CVE-2026-3177",
		"image_ref":   "registry.example/api:prod",
		"workload_id": "workload:api",
		"service_id":  "service:api",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/supply-chain/impact/explain"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["workload_id"], "workload:api"; got != want {
		t.Fatalf("route.query[workload_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["service_id"], "service:api"; got != want {
		t.Fatalf("route.query[service_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["image_ref"], "registry.example/api:prod"; got != want {
		t.Fatalf("route.query[image_ref] = %#v, want %#v", got, want)
	}
}

func TestDispatchToolSupplyChainImpactExplainReturnsEnvelope(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/supply-chain/impact/explain", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), "application/eshu.envelope+json"; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("finding_id"), "finding-1"; got != want {
			t.Fatalf("finding_id = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"outcome": "finding_explained",
				"finding": map[string]any{"finding_id": "finding-1", "impact_status": "affected_exact"},
				"advisory": map[string]any{
					"cve_id":           "CVE-2026-0001",
					"vulnerable_range": "<2.0.0",
				},
				"component": map[string]any{
					"package_id":       "pkg:npm/left-pad",
					"observed_version": "1.2.3",
				},
				"version": map[string]any{
					"fixed_version":    "2.0.0",
					"version_evidence": "exact",
				},
				"anchors": map[string]any{
					"manifest_paths": []string{"package-lock.json"},
				},
				"freshness": map[string]any{"state": "fresh"},
			},
			"truth": map[string]any{
				"level":      "exact",
				"capability": "supply_chain.impact_explanation.read",
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
		"explain_supply_chain_impact",
		map[string]any{"finding_id": "finding-1"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want explain envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %T, want map[string]any", result.Envelope.Data)
	}
	if got, want := data["outcome"], "finding_explained"; got != want {
		t.Fatalf("outcome = %#v, want %#v", got, want)
	}
}

func TestDispatchToolSupplyChainImpactExplainPreservesRefusalEnvelope(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/supply-chain/impact/explain", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Query().Get("workload_id"), "workload:api"; got != want {
			t.Fatalf("workload_id = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"outcome":                "no_finding",
				"evidence_packet_handle": "supply-chain-impact-explanation:scope:abc123",
				"readiness": map[string]any{
					"readiness_state": "unsupported",
					"unsupported_targets": []map[string]any{
						{"target_kind": "ecosystem", "reason": "unsupported_ecosystem", "ecosystem": "pypi"},
					},
					"source_states": []map[string]any{
						{"source": "osv", "freshness_state": "partial", "last_error_class": "permission_hidden"},
					},
					"missing_evidence": []string{"unsupported_targets"},
				},
				"missing_evidence": []string{"impact_finding", "unsupported_targets"},
			},
			"truth": map[string]any{
				"level":      "exact",
				"capability": "supply_chain.impact_explanation.read",
				"profile":    "production",
				"basis":      "semantic_facts",
			},
			"error": nil,
		})
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"explain_supply_chain_impact",
		map[string]any{"cve_id": "CVE-2026-3177", "workload_id": "workload:api"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %T, want map[string]any", result.Envelope.Data)
	}
	if got, want := data["outcome"], "no_finding"; got != want {
		t.Fatalf("outcome = %#v, want %#v", got, want)
	}
	if got, want := data["evidence_packet_handle"], "supply-chain-impact-explanation:scope:abc123"; got != want {
		t.Fatalf("evidence_packet_handle = %#v, want %#v", got, want)
	}
	readiness, ok := data["readiness"].(map[string]any)
	if !ok {
		t.Fatalf("readiness = %T, want map[string]any", data["readiness"])
	}
	if got, want := readiness["readiness_state"], "unsupported"; got != want {
		t.Fatalf("readiness_state = %#v, want %#v", got, want)
	}
}

func TestDispatchToolSupplyChainImpactExplainPreservesAmbiguousScopeEnvelope(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/supply-chain/impact/explain", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Query().Get("repository_id"), "repo://example/api"; got != want {
			t.Fatalf("repository_id = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"outcome":                "ambiguous_scope",
				"evidence_packet_handle": "supply-chain-impact-explanation:scope:ambiguous",
				"version": map[string]any{
					"version_evidence": "missing",
				},
				"readiness": map[string]any{
					"readiness_state": "ready_zero_findings",
				},
				"missing_evidence": []string{"ambiguous_scope"},
			},
			"truth": map[string]any{
				"level":      "exact",
				"capability": "supply_chain.impact_explanation.read",
				"profile":    "production",
				"basis":      "semantic_facts",
			},
			"error": nil,
		})
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"explain_supply_chain_impact",
		map[string]any{"advisory_id": "GHSA-ambiguous", "repository_id": "repo://example/api"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %T, want map[string]any", result.Envelope.Data)
	}
	if got, want := data["outcome"], "ambiguous_scope"; got != want {
		t.Fatalf("outcome = %#v, want %#v", got, want)
	}
	if got, want := data["evidence_packet_handle"], "supply-chain-impact-explanation:scope:ambiguous"; got != want {
		t.Fatalf("evidence_packet_handle = %#v, want %#v", got, want)
	}
	if !stringSliceContains(stringsOf(data["missing_evidence"]), "ambiguous_scope") {
		t.Fatalf("missing_evidence = %#v, want ambiguous_scope", data["missing_evidence"])
	}
}
