// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// Code-route OpenAPI proofs that live in package query: they assemble the
// full OpenAPISpec, which is root-owned assembly codequery cannot name
// without importing the root back (#6060). Split from
// codequery/cypher_handler_test.go at the lane-A move; the
// handler-behavior cypher proofs stay there.

func TestOpenAPICypherRouteDocumentsUnsupportedProfile(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/code/cypher")
	post := querytestutil.MustMapField(t, path, "post")
	responses := querytestutil.MustMapField(t, post, "responses")
	if _, ok := responses["501"]; !ok {
		t.Fatalf("Cypher OpenAPI responses missing 501 unsupported profile response")
	}
}

func TestOpenAPICypherRouteDocumentsBoundedGraphReadFailures(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/code/cypher")
	post := querytestutil.MustMapField(t, path, "post")
	responses := querytestutil.MustMapField(t, post, "responses")
	for _, status := range []string{"503", "504"} {
		if _, ok := responses[status]; !ok {
			t.Errorf("Cypher OpenAPI responses missing %s bounded graph-read response", status)
		}
	}
}

func TestOpenAPIVisualizeRouteDocumentsUnsupportedProfile(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/code/visualize")
	post := querytestutil.MustMapField(t, path, "post")
	responses := querytestutil.MustMapField(t, post, "responses")
	if _, ok := responses["501"]; !ok {
		t.Fatalf("Visualize OpenAPI responses missing 501 unsupported profile response")
	}
}

func TestOpenAPIVisualizeRouteDocumentsBoundedGraphReadFailures(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/code/visualize")
	post := querytestutil.MustMapField(t, path, "post")
	responses := querytestutil.MustMapField(t, post, "responses")
	for _, status := range []string{"503", "504"} {
		if _, ok := responses[status]; !ok {
			t.Errorf("Visualize OpenAPI responses missing %s bounded graph-read response", status)
		}
	}
}

func TestOpenAPIVisualizeRouteCarriesSharedKeyOnlyMarker(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/code/visualize")
	post := querytestutil.MustMapField(t, path, "post")
	marked, ok := post["x-shared-key-only"].(bool)
	if !ok || !marked {
		t.Fatalf(`Visualize OpenAPI operation missing "x-shared-key-only": true marker`)
	}
}
