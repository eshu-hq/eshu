// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestSBOMAttestationAttachmentAggregateToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"count_sbom_attestation_attachments", "get_sbom_attestation_attachment_inventory"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		for _, field := range []string{"subject_digest", "document_id", "document_digest", "repository_id", "workload_id", "service_id", "attachment_status", "artifact_kind"} {
			if _, ok := properties[field]; !ok {
				t.Fatalf("tool %s properties missing %q", name, field)
			}
		}
	}
}

func TestResolveRouteMapsCountSBOMAttestationAttachments(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("count_sbom_attestation_attachments", map[string]any{
		"repository_id":     "repo-1",
		"attachment_status": "attached_verified",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/sbom-attestations/attachments/count"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetSBOMAttestationAttachmentInventory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_sbom_attestation_attachment_inventory", map[string]any{
		"repository_id": "repo-1",
		"group_by":      "attachment_status",
		"limit":         float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/sbom-attestations/attachments/inventory"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
