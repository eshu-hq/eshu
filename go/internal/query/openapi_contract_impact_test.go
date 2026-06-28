// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPIContractImpactSurfaceDocumentsFamiliesAndEvidenceBoundary(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	contractPath := mustMapField(t, paths, "/api/v0/impact/contracts")
	contractPost := mustMapField(t, contractPath, "post")
	description, ok := contractPost["description"].(string)
	if !ok {
		t.Fatal("contract impact description missing or not a string")
	}
	for _, want := range []string{
		"deterministic parser, spec, or config evidence",
		"does not infer contract edges from string similarity",
		"topic and grpc return explicit unsupported family states",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("contract impact description missing %q: %s", want, description)
		}
	}

	requestBody := mustMapField(t, mustMapField(t, contractPost, "requestBody"), "content")
	requestJSON := mustMapField(t, requestBody, "application/json")
	requestSchema := mustMapField(t, requestJSON, "schema")
	requestProperties := mustMapField(t, requestSchema, "properties")
	for _, field := range []string{
		"family",
		"provider_repo_id",
		"consumer_repo_id",
		"repo_id",
		"route",
		"topic",
		"service_name",
		"method",
		"limit",
	} {
		if _, ok := requestProperties[field]; !ok {
			t.Fatalf("contract impact request schema missing %q", field)
		}
	}
	family := mustMapField(t, requestProperties, "family")
	enum, ok := family["enum"].([]any)
	if !ok {
		t.Fatalf("family enum type = %T, want []any", family["enum"])
	}
	for _, want := range []string{"http", "topic", "grpc"} {
		if !containsValue(enum, want) {
			t.Fatalf("family enum = %#v, want %q", enum, want)
		}
	}
	limit := mustMapField(t, requestProperties, "limit")
	if got, want := limit["maximum"], float64(100); got != want {
		t.Fatalf("limit maximum = %#v, want %#v", got, want)
	}
	anyOf, ok := requestSchema["anyOf"].([]any)
	if !ok {
		t.Fatalf("anyOf type = %T, want []any", requestSchema["anyOf"])
	}
	for _, raw := range anyOf {
		alternative := raw.(map[string]any)
		required := alternative["required"].([]any)
		if containsValue(required, "route") {
			t.Fatalf("contract impact schema must not advertise route-only scope: %#v", anyOf)
		}
	}

	responses := mustMapField(t, contractPost, "responses")
	okResponse := mustMapField(t, responses, "200")
	responseJSON := mustMapField(t, mustMapField(t, okResponse, "content"), "application/json")
	responseProperties := mustMapField(t, mustMapField(t, responseJSON, "schema"), "properties")
	for _, field := range []string{"family", "scope", "families", "providers", "consumers", "coverage", "truncated"} {
		if _, ok := responseProperties[field]; !ok {
			t.Fatalf("contract impact response schema missing %q", field)
		}
	}
	families := mustMapField(t, responseProperties, "families")
	if got, want := families["type"], "object"; got != want {
		t.Fatalf("families type = %#v, want %#v", got, want)
	}
	for _, status := range []string{"400", "501", "503", "500"} {
		if _, ok := responses[status]; !ok {
			t.Fatalf("contract impact responses missing status %s", status)
		}
	}
}
