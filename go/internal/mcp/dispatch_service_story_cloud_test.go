// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestDispatchToolServiceStoryCarriesCloudResources(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/services/orders-api/story", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), "application/eshu.envelope+json"; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"service_identity": map[string]any{"service_id": "workload:orders-api"},
				"cloud_resources": []map[string]any{
					{
						"id":                 "cloud-resource:orders-listener",
						"relationship_basis": "aws_resource_service_anchor",
					},
				},
			},
			"truth": map[string]any{"level": "exact"},
			"error": nil,
		})
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"get_service_story",
		map[string]any{"workload_id": "workload:orders-api"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("Envelope.Data type = %T, want map", result.Envelope.Data)
	}
	resources, ok := data["cloud_resources"].([]any)
	if !ok {
		t.Fatalf("cloud_resources type = %T, want []any", data["cloud_resources"])
	}
	if got, want := len(resources), 1; got != want {
		t.Fatalf("len(cloud_resources) = %d, want %d", got, want)
	}
}

func TestDispatchToolServiceStoryPreservesCloudDependencyTrace(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/services/orders-api/story", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), "application/eshu.envelope+json"; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"service_identity": map[string]any{"service_id": "workload:orders-api"},
				"code_to_runtime_trace": map[string]any{
					"status":           "partial",
					"missing_segments": []string{"cloud_dependencies"},
					"segments": []map[string]any{
						{
							"name":                 "cloud_dependencies",
							"status":               "missing_evidence",
							"basis":                "uncorrelated_cloud_resource_candidates",
							"evidence_count":       1,
							"candidate_count":      1,
							"promoted_count":       0,
							"missing_relationship": "workload_cloud_relationship",
							"missing_evidence":     []string{"stale_deployment_evidence"},
							"evidence": []map[string]any{
								{
									"id":                    "cloud-resource:old-queue",
									"candidate_status":      "stale_anchor",
									"service_anchor_reason": "stale_deployment_evidence",
								},
							},
						},
					},
				},
			},
			"truth": map[string]any{"level": "partial"},
			"error": nil,
		})
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"get_service_story",
		map[string]any{"workload_id": "workload:orders-api"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("Envelope.Data type = %T, want map", result.Envelope.Data)
	}
	trace := mcpMapValue(data, "code_to_runtime_trace")
	segments := mcpMapSliceValue(trace, "segments")
	cloud := mcpCloudDependencySegment(segments)
	if cloud == nil {
		t.Fatalf("cloud_dependencies segment missing: %#v", trace)
	}
	if got, want := query.StringVal(cloud, "status"), "missing_evidence"; got != want {
		t.Fatalf("cloud_dependencies.status = %q, want %q", got, want)
	}
	if got, want := query.IntVal(cloud, "promoted_count"), 0; got != want {
		t.Fatalf("cloud_dependencies.promoted_count = %d, want %d", got, want)
	}
	missing := query.StringSliceVal(cloud, "missing_evidence")
	if len(missing) != 1 || missing[0] != "stale_deployment_evidence" {
		t.Fatalf("cloud_dependencies.missing_evidence = %#v, want stale_deployment_evidence", missing)
	}
	evidence := mcpMapSliceValue(cloud, "evidence")
	if got, want := query.StringVal(evidence[0], "candidate_status"), "stale_anchor"; got != want {
		t.Fatalf("cloud_dependencies.evidence[0].candidate_status = %q, want %q", got, want)
	}
}

func mcpCloudDependencySegment(segments []map[string]any) map[string]any {
	for _, segment := range segments {
		if query.StringVal(segment, "name") == "cloud_dependencies" {
			return segment
		}
	}
	return nil
}
