// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestResolveRouteMapsSupplyChainImpactCountProfile(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("count_supply_chain_impact_findings", map[string]any{
		"repository_id": "repo://example/api",
		"profile":       "comprehensive",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/impact/findings/count"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["repository_id"], "repo://example/api"; got != want {
		t.Fatalf("route.query[repository_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["profile"], "comprehensive"; got != want {
		t.Fatalf("route.query[profile] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsSupplyChainImpactCountDefaultProfileToHTTPDefault(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("count_supply_chain_impact_findings", map[string]any{
		"repository_id": "repo://example/api",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.query["profile"], ""; got != want {
		t.Fatalf("route.query[profile] = %#v, want empty so HTTP applies precise default", got)
	}
}

func TestResolveRouteMapsContainerImageAggregatesForwardSourceRepositoryScope(t *testing.T) {
	t.Parallel()

	for _, toolName := range []string{
		"count_container_image_identities",
		"get_container_image_identity_inventory",
	} {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			t.Parallel()

			route, err := resolveRoute(toolName, map[string]any{
				"source_repository_id": "repo://example/api",
			})
			if err != nil {
				t.Fatalf("resolveRoute() error = %v, want nil", err)
			}
			if got, want := route.query["source_repository_id"], "repo://example/api"; got != want {
				t.Fatalf("route.query[source_repository_id] = %#v, want %#v", got, want)
			}
			if got, want := route.query["repository_id"], ""; got != want {
				t.Fatalf("route.query[repository_id] = %#v, want empty OCI scope", got)
			}
		})
	}
}

func TestContainerImageAggregateToolSchemasAdvertiseSourceRepositoryScope(t *testing.T) {
	t.Parallel()

	for _, tool := range containerImageIdentityAggregateTools() {
		schema := tool.InputSchema.(map[string]any)
		properties := schema["properties"].(map[string]any)
		sourceRepository := properties["source_repository_id"].(map[string]any)
		description := sourceRepository["description"].(string)
		for _, want := range []string{"source repository", "not an OCI"} {
			if !strings.Contains(description, want) {
				t.Fatalf("%s source_repository_id description = %q, want %q", tool.Name, description, want)
			}
		}
	}
}

func TestResolveRouteMapsSupplyChainImpactAggregatePriorityAndSuppressionFilters(t *testing.T) {
	t.Parallel()

	for _, toolName := range []string{
		"count_supply_chain_impact_findings",
		"get_supply_chain_impact_inventory",
	} {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			t.Parallel()

			route, err := resolveRoute(toolName, map[string]any{
				"repository_id":      "repo://example/api",
				"profile":            "comprehensive",
				"priority_bucket":    "high",
				"min_priority_score": float64(75),
				"suppression_state":  "accepted_risk",
				"include_suppressed": true,
			})
			if err != nil {
				t.Fatalf("resolveRoute() error = %v, want nil", err)
			}
			for key, want := range map[string]string{
				"profile":            "comprehensive",
				"priority_bucket":    "high",
				"min_priority_score": "75",
				"suppression_state":  "accepted_risk",
				"include_suppressed": "true",
			} {
				if got := route.query[key]; got != want {
					t.Fatalf("route.query[%s] = %#v, want %#v", key, got, want)
				}
			}
		})
	}
}

func TestDispatchToolSupplyChainImpactCountPreservesProfile(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/supply-chain/impact/findings/count", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Query().Get("repository_id"), "repo://example/api"; got != want {
			t.Fatalf("repository_id = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("profile"), "comprehensive"; got != want {
			t.Fatalf("profile = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"total_findings":    3,
				"affected_findings": 2,
				"detection_profile": "comprehensive",
			},
			"truth": map[string]any{
				"level":      "exact",
				"capability": "supply_chain.impact_findings.aggregate",
				"profile":    "production",
				"basis":      "semantic_facts",
			},
			"error": nil,
		})
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"count_supply_chain_impact_findings",
		map[string]any{
			"repository_id": "repo://example/api",
			"profile":       "comprehensive",
		},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want aggregate envelope")
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope.Data = %T, want map[string]any", result.Envelope.Data)
	}
	if got, want := data["detection_profile"], "comprehensive"; got != want {
		t.Fatalf("detection_profile = %#v, want %#v", got, want)
	}
	if got, want := data["total_findings"], float64(3); got != want {
		t.Fatalf("total_findings = %#v, want %#v", got, want)
	}
}
