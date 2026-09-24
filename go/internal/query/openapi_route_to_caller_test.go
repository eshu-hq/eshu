// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestOpenAPIRouteToCallerExposesExactRouteTraceContract(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	routePath := testutil.MustMapField(t, paths, "/api/v0/code/routes/callers")
	post := testutil.MustMapField(t, routePath, "post")
	if got, want := post["operationId"], "traceRouteCallers"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}

	body := testutil.MustMapField(t, testutil.MustMapField(t, post, "requestBody"), "content")
	jsonBody := testutil.MustMapField(t, body, "application/json")
	request := testutil.MustMapField(t, testutil.MustMapField(t, jsonBody, "schema"), "properties")
	for _, field := range []string{"repo_id", "service_id", "service_name", "method", "path", "max_depth", "limit"} {
		if _, ok := request[field]; !ok {
			t.Fatalf("route-to-caller request schema missing %s", field)
		}
	}

	responses := testutil.MustMapField(t, post, "responses")
	okResp := testutil.MustMapField(t, responses, "200")
	content := testutil.MustMapField(t, testutil.MustMapField(t, okResp, "content"), "application/json")
	response := testutil.MustMapField(t, testutil.MustMapField(t, content, "schema"), "properties")
	for _, field := range []string{"status", "truncated", "unsupported", "route", "handler", "callers", "callees", "impact", "truth_source"} {
		if _, ok := response[field]; !ok {
			t.Fatalf("route-to-caller response schema missing %s", field)
		}
	}
	if _, ok := responses["409"]; !ok {
		t.Fatal("route-to-caller responses missing 409 ambiguous selector response")
	}
	if _, ok := responses["501"]; !ok {
		t.Fatal("route-to-caller responses missing 501 unsupported capability response")
	}
}
