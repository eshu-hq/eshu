// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestOpenAPIDeploymentConfigInfluenceDocumentsBoundsAndAmbiguity(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	ServeOpenAPI(recorder, httptest.NewRequest("GET", "/api/v0/openapi.json", nil))

	var spec map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode OpenAPI document: %v", err)
	}
	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/impact/deployment-config-influence")
	post := mustMapField(t, path, "post")
	responses := mustMapField(t, post, "responses")
	if _, ok := responses["409"]; !ok {
		t.Fatal("deployment-config-influence responses missing 409 ambiguity response")
	}
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, okResponse, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	properties := mustMapField(t, schema, "properties")

	for _, field := range []string{"deployment_source_limits", "k8s_resource_limits"} {
		limits := mustMapField(t, properties, field)
		limitProperties := mustMapField(t, limits, "properties")
		for _, limitField := range []string{
			"limit",
			"query_sentinel_limit",
			"returned_count",
			"observed_count",
			"observed_count_is_lower_bound",
			"truncated",
			"ordering",
		} {
			if _, ok := limitProperties[limitField]; !ok {
				t.Fatalf("%s schema missing %s", field, limitField)
			}
		}
	}

	k8sLimits := mustMapField(t, properties, "k8s_resource_limits")
	k8sProperties := mustMapField(t, k8sLimits, "properties")
	for _, field := range []string{
		"deployment_source_query_sentinel_limit",
		"content_observed_count",
		"content_observed_count_is_lower_bound",
		"deployment_source_observed_count",
		"deployment_source_observed_count_is_lower_bound",
	} {
		if _, ok := k8sProperties[field]; !ok {
			t.Fatalf("k8s_resource_limits schema missing %s", field)
		}
	}

	coverage := mustMapField(t, properties, "coverage")
	coverageProperties := mustMapField(t, coverage, "properties")
	for _, field := range []string{"truncated", "observed_count_is_lower_bound"} {
		if _, ok := coverageProperties[field]; !ok {
			t.Fatalf("coverage schema missing %s", field)
		}
	}
}
