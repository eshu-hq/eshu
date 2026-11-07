// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package docs

import (
	"testing"
)

// TestHTTPEndpointTruthDrainsQueryImport pins the doc-truth seam after the
// #9669 row-3 drain: endpoint truth comes from the handler-free OpenAPI
// assembly leaf (the same document query.OpenAPISpec forwards), never from
// the handler package. The pinned route exists only in the assembled spec,
// so an empty or wrong document fails this test instead of silently
// verifying docs against no endpoints.
func TestHTTPEndpointTruthDrainsQueryImport(t *testing.T) {
	truth := HTTPEndpointTruth()
	if len(truth) == 0 {
		t.Fatal("expected non-empty HTTP endpoint truth")
	}
	found := false
	for _, endpoint := range truth {
		if endpoint.Method == "GET" && endpoint.Path == "/api/v0/openapi.json" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected GET /api/v0/openapi.json from the assembled spec")
	}
}

// TestEndpointTruthFromOpenAPIFailClosed pins the fail-closed decode: a
// spec that will not parse yields no endpoints rather than an error, so
// documentation claims read as missing evidence, not contradicted.
func TestEndpointTruthFromOpenAPIFailClosed(t *testing.T) {
	if got := endpointTruthFromOpenAPI("not json"); len(got) != 0 {
		t.Fatalf("expected no endpoints from garbage spec, got %v", got)
	}
	if got := endpointTruthFromOpenAPI(`{"paths":{}}`); len(got) != 0 {
		t.Fatalf("expected no endpoints from empty paths, got %v", got)
	}
}
