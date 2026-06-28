// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apirecording_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/replay/apirecording"
)

// TestRecordedPathsAreDeclaredInOpenAPI proves the recorded HTTP exchanges stay
// in lockstep with the OpenAPI spec: a recording cannot assert a route the spec
// does not declare. The spec is the canonical source of truth for the public
// /api/v0 contract (per docs/public/reference/http-api.md, which is a delegating
// route map rather than a per-route enumeration — so a recorded-path substring
// check against that page would be brittle and is intentionally not done here;
// the OpenAPI spec is the machine-readable contract the gate binds to).
func TestRecordedPathsAreDeclaredInOpenAPI(t *testing.T) {
	specPaths := openAPIPaths(t)

	for _, req := range recordingRequests() {
		if req.Transport != apirecording.TransportHTTP && req.Transport != "" {
			// Non-HTTP transports (e.g. MCP tool calls in R-9) dispatch through the
			// mux but are not declared as OpenAPI paths; skip the path-lockstep
			// check for them.
			continue
		}
		route := pathWithoutQuery(req.Path)
		if _, ok := specPaths[route]; !ok {
			t.Fatalf("recorded path %q (exchange %q) is not declared in OpenAPISpec(); a recording must not reference an undeclared route", route, req.Name)
		}
	}
}

// openAPIPaths decodes OpenAPISpec() and returns the set of declared paths.
func openAPIPaths(t *testing.T) map[string]struct{} {
	t.Helper()
	var spec struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(query.OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths := make(map[string]struct{}, len(spec.Paths))
	for p := range spec.Paths {
		paths[p] = struct{}{}
	}
	return paths
}

// pathWithoutQuery strips a query string from a recorded path so it matches the
// OpenAPI path key (which never includes a query string).
func pathWithoutQuery(path string) string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		return path[:i]
	}
	return path
}
