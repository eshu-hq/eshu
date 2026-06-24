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
)

func TestDispatchToolServiceStoryPreservesSupplyChainTrace(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/services/api/story", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), "application/eshu.envelope+json"; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"code_to_runtime_trace": map[string]any{
					"segments": []map[string]any{
						{
							"name":   "image_package",
							"status": "exact",
							"basis":  "container_image_identity_and_sbom_attachment",
							"evidence": []map[string]any{
								{
									"image_ref":                  "registry.example.com/team/api:prod",
									"digest":                     "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
									"sbom_attachment_id":         "sbom-attachment-1",
									"sbom_attachment_status":     "attached_verified",
									"sbom_evidence_fact_ids":     []string{"sbom-referrer"},
									"identity_evidence_fact_ids": []string{"oci-tag-observation"},
								},
							},
							"missing_evidence_details": []map[string]any{
								{
									"candidate_image_ref":     "registry.example.com/team/worker:prod",
									"candidate_repository_id": "oci-registry://registry.example.com/team/worker",
									"collector_scope":         "outside_configured_targets",
									"reason":                  "oci_registry_target_outside_scope",
									"operator_action":         "add an OCI registry collector target for oci-registry://registry.example.com/team/worker",
								},
							},
						},
					},
				},
			},
			"truth": map[string]any{
				"level":      "exact",
				"capability": "platform_impact.context_overview",
				"profile":    "production",
				"basis":      "hybrid",
				"freshness":  map[string]any{"state": "fresh"},
			},
			"error": nil,
		})
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"get_service_story",
		map[string]any{"workload_id": "workload:api"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil || result.Envelope.Data == nil {
		t.Fatalf("dispatchTool() envelope = %#v, want structured service story data", result.Envelope)
	}
	data, ok := result.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want object", result.Envelope.Data)
	}
	trace, ok := data["code_to_runtime_trace"].(map[string]any)
	if !ok {
		t.Fatalf("code_to_runtime_trace = %#v, want object", data["code_to_runtime_trace"])
	}
	segments, ok := trace["segments"].([]any)
	if !ok || len(segments) != 1 {
		t.Fatalf("segments = %#v, want one image_package segment", trace["segments"])
	}
	segment, ok := segments[0].(map[string]any)
	if !ok {
		t.Fatalf("segment type = %T, want object", segments[0])
	}
	if got, want := segment["status"], "exact"; got != want {
		t.Fatalf("image_package status = %#v, want %#v", got, want)
	}
	evidence, ok := segment["evidence"].([]any)
	if !ok || len(evidence) != 1 {
		t.Fatalf("evidence = %#v, want one exact row", segment["evidence"])
	}
	row, ok := evidence[0].(map[string]any)
	if !ok {
		t.Fatalf("evidence row type = %T, want object", evidence[0])
	}
	if got, want := row["sbom_attachment_id"], "sbom-attachment-1"; got != want {
		t.Fatalf("sbom_attachment_id = %#v, want %#v", got, want)
	}
	details, ok := segment["missing_evidence_details"].([]any)
	if !ok || len(details) != 1 {
		t.Fatalf("missing_evidence_details = %#v, want one detail row", segment["missing_evidence_details"])
	}
	detail, ok := details[0].(map[string]any)
	if !ok {
		t.Fatalf("missing_evidence detail type = %T, want object", details[0])
	}
	if got, want := detail["reason"], "oci_registry_target_outside_scope"; got != want {
		t.Fatalf("missing_evidence detail reason = %#v, want %#v", got, want)
	}
}
