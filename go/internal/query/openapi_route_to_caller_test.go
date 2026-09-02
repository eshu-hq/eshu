// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPIRouteToCallerExposesExactRouteTraceContract(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	routePath := querytestutil.MustMapField(t, paths, "/api/v0/code/routes/callers")
	post := querytestutil.MustMapField(t, routePath, "post")
	if got, want := post["operationId"], "traceRouteCallers"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}

	body := querytestutil.MustMapField(t, querytestutil.MustMapField(t, post, "requestBody"), "content")
	jsonBody := querytestutil.MustMapField(t, body, "application/json")
	request := querytestutil.MustMapField(t, querytestutil.MustMapField(t, jsonBody, "schema"), "properties")
	for _, field := range []string{"repo_id", "service_id", "service_name", "method", "path", "max_depth", "limit"} {
		if _, ok := request[field]; !ok {
			t.Fatalf("route-to-caller request schema missing %s", field)
		}
	}

	responses := querytestutil.MustMapField(t, post, "responses")
	okResp := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, querytestutil.MustMapField(t, okResp, "content"), "application/json")
	response := querytestutil.MustMapField(t, querytestutil.MustMapField(t, content, "schema"), "properties")
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
