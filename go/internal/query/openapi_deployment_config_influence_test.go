// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPIDeploymentConfigInfluenceDocumentsBoundsAndAmbiguity(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	ServeOpenAPI(recorder, httptest.NewRequest("GET", "/api/v0/openapi.json", nil))

	var spec map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode OpenAPI document: %v", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	path := testutil.MustMapField(t, paths, "/api/v0/impact/deployment-config-influence")
	post := testutil.MustMapField(t, path, "post")
	responses := testutil.MustMapField(t, post, "responses")
	if _, ok := responses["409"]; !ok {
		t.Fatal("deployment-config-influence responses missing 409 ambiguity response")
	}
	okResponse := testutil.MustMapField(t, responses, "200")
	content := testutil.MustMapField(t, okResponse, "content")
	jsonContent := testutil.MustMapField(t, content, "application/json")
	schema := testutil.MustMapField(t, jsonContent, "schema")
	properties := testutil.MustMapField(t, schema, "properties")

	for _, field := range []string{"deployment_source_limits", "k8s_resource_limits"} {
		limits := testutil.MustMapField(t, properties, field)
		limitProperties := testutil.MustMapField(t, limits, "properties")
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

	k8sLimits := testutil.MustMapField(t, properties, "k8s_resource_limits")
	k8sProperties := testutil.MustMapField(t, k8sLimits, "properties")
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

	coverage := testutil.MustMapField(t, properties, "coverage")
	coverageProperties := testutil.MustMapField(t, coverage, "properties")
	for _, field := range []string{"truncated", "observed_count_is_lower_bound"} {
		if _, ok := coverageProperties[field]; !ok {
			t.Fatalf("coverage schema missing %s", field)
		}
	}
}
