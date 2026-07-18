// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestOpenAPIImpactDeploymentTraceDocumentsCanonicalPlatformIdentity(t *testing.T) {
	recorder := httptest.NewRecorder()
	ServeOpenAPI(recorder, httptest.NewRequest("GET", "/api/v0/openapi.json", nil))

	var spec map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode OpenAPI document: %v", err)
	}
	paths := mustMapField(t, spec, "paths")
	tracePath := mustMapField(t, paths, "/api/v0/impact/trace-deployment-chain")
	tracePost := mustMapField(t, tracePath, "post")
	responses := mustMapField(t, tracePost, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, okResponse, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	properties := mustMapField(t, schema, "properties")
	instances := mustMapField(t, properties, "instances")
	instanceItems := mustMapField(t, instances, "items")
	instanceProperties := mustMapField(t, instanceItems, "properties")
	platforms := mustMapField(t, instanceProperties, "platforms")
	platformItems := mustMapField(t, platforms, "items")
	platformProperties := mustMapField(t, platformItems, "properties")
	if _, ok := platformProperties["platform_id"]; !ok {
		t.Fatal("impact trace instances[].platforms[] schema missing platform_id")
	}
}
