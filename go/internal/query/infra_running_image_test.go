// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSearchInfraResourcesSurfacesRunningImageOnCloudResources proves the
// running_image_ref/running_image_digest CloudResource node props (issue
// #5450) reach find_infra_resources's response, so an operator searching for
// an ECS running task or Lambda function CloudResource sees the running image
// inline without a separate graph traversal.
func TestSearchInfraResourcesSurfacesRunningImageOnCloudResources(t *testing.T) {
	t.Parallel()

	reader := &recordingInfraGraphReader{
		runRows: []map[string]any{
			{
				"id":                   "cloud-resource:lambda-demo",
				"name":                 "demo",
				"labels":               []any{"CloudResource"},
				"provider":             "aws",
				"resource_type":        "lambda.function",
				"resource_id":          "arn:aws:lambda:us-east-1:123456789012:function:demo",
				"account_id":           "123456789012",
				"region":               "us-east-1",
				"service_kind":         "lambda",
				"source":               "aws",
				"running_image_ref":    "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
				"running_image_digest": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc",
			},
		},
	}
	handler := &InfraHandler{Neo4j: reader}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"query":"demo","category":"cloud","provider":"aws","resource_service":"lambda","limit":5}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if !strings.Contains(reader.lastCypher, "coalesce(n.running_image_ref, '') as running_image_ref") {
		t.Fatalf("cypher = %q, want running_image_ref column", reader.lastCypher)
	}
	if !strings.Contains(reader.lastCypher, "coalesce(n.running_image_digest, '') as running_image_digest") {
		t.Fatalf("cypher = %q, want running_image_digest column", reader.lastCypher)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	results := resp["results"].([]any)
	if got, want := len(results), 1; got != want {
		t.Fatalf("results len = %d, want %d", got, want)
	}
	resource := results[0].(map[string]any)
	for key, want := range map[string]any{
		"running_image_ref":    "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
		"running_image_digest": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc",
	} {
		if got := resource[key]; got != want {
			t.Fatalf("%s = %#v, want %#v", key, got, want)
		}
	}
}

// TestSearchInfraResourcesOmitsRunningImageWhenAbsent proves a CloudResource
// with no running image evidence (the common case for non-ECS/Lambda
// resources) never emits the running_image_ref/running_image_digest keys,
// matching every other optional field's non-empty-only convention.
func TestSearchInfraResourcesOmitsRunningImageWhenAbsent(t *testing.T) {
	t.Parallel()

	reader := &recordingInfraGraphReader{
		runRows: []map[string]any{
			{
				"id":            "cloud-resource:ssm-parameter",
				"name":          "/configd/sample-service/database",
				"labels":        []any{"CloudResource"},
				"provider":      "aws",
				"resource_type": "aws_ssm_parameter",
				"resource_id":   "/configd/sample-service/database",
				"service_kind":  "ssm",
				"source":        "aws",
			},
		},
	}
	handler := &InfraHandler{Neo4j: reader}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/infra/resources/search",
		bytes.NewBufferString(`{"query":"sample-service","category":"cloud","limit":5}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	results := resp["results"].([]any)
	resource := results[0].(map[string]any)
	if _, ok := resource["running_image_ref"]; ok {
		t.Fatalf("running_image_ref = %v, want absent when the node carries no running image", resource["running_image_ref"])
	}
	if _, ok := resource["running_image_digest"]; ok {
		t.Fatalf("running_image_digest = %v, want absent when the node carries no running image", resource["running_image_digest"])
	}
}
