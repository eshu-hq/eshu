// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesReplatformingOwnership(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/replatforming/ownership-packets")
	post := mustMapField(t, path, "post")
	if got, want := post["operationId"], "composeReplatformingOwnershipPackets"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	responses := mustMapField(t, post, "responses")
	ok := mustMapField(t, responses, "200")
	content := mustMapField(t, ok, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	properties := mustMapField(t, schema, "properties")
	if _, ok := properties["ownership_packets"]; !ok {
		t.Fatal("ownership response schema missing ownership_packets")
	}
	if _, ok := properties["ambiguous_count"]; !ok {
		t.Fatal("ownership response schema missing ambiguous_count")
	}

	components := mustMapField(t, spec, "components")
	schemas := mustMapField(t, components, "schemas")
	if _, ok := schemas["ReplatformingOwnershipPacket"]; !ok {
		t.Fatal("components.schemas missing ReplatformingOwnershipPacket")
	}
	if _, ok := schemas["ReplatformingOwnerCandidate"]; !ok {
		t.Fatal("components.schemas missing ReplatformingOwnerCandidate")
	}
}
