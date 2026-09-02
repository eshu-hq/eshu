// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecIncludesReplatformingOwnership(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/replatforming/ownership-packets")
	post := querytestutil.MustMapField(t, path, "post")
	if got, want := post["operationId"], "composeReplatformingOwnershipPackets"; got != want {
		t.Fatalf("operationId = %q, want %q", got, want)
	}
	responses := querytestutil.MustMapField(t, post, "responses")
	ok := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, ok, "content")
	jsonContent := querytestutil.MustMapField(t, content, "application/json")
	schema := querytestutil.MustMapField(t, jsonContent, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	if _, ok := properties["ownership_packets"]; !ok {
		t.Fatal("ownership response schema missing ownership_packets")
	}
	if _, ok := properties["ambiguous_count"]; !ok {
		t.Fatal("ownership response schema missing ambiguous_count")
	}

	components := querytestutil.MustMapField(t, spec, "components")
	schemas := querytestutil.MustMapField(t, components, "schemas")
	if _, ok := schemas["ReplatformingOwnershipPacket"]; !ok {
		t.Fatal("components.schemas missing ReplatformingOwnershipPacket")
	}
	if _, ok := schemas["ReplatformingOwnerCandidate"]; !ok {
		t.Fatal("components.schemas missing ReplatformingOwnerCandidate")
	}
}
