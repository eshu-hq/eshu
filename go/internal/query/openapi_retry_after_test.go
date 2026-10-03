// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

// TestOpenAPIServiceUnavailableDeclaresRetryAfter pins #7523: the shared 503
// response every dead-code, dead-IaC, and impact-findings route references
// must declare the Retry-After header, and POST /api/v0/iac/dead and
// GET /api/v0/supply-chain/impact/findings must reference it.
func TestOpenAPIServiceUnavailableDeclaresRetryAfter(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	components, _ := spec["components"].(map[string]any)
	responses, _ := components["responses"].(map[string]any)
	unavailable, _ := responses["ServiceUnavailable"].(map[string]any)
	headers, _ := unavailable["headers"].(map[string]any)
	if _, ok := headers["Retry-After"]; !ok {
		t.Fatalf("components.responses.ServiceUnavailable has no Retry-After header: %#v", unavailable)
	}

	paths, _ := spec["paths"].(map[string]any)
	path, _ := paths["/api/v0/iac/dead"].(map[string]any)
	post, _ := path["post"].(map[string]any)
	opResponses, _ := post["responses"].(map[string]any)
	ref, _ := opResponses["503"].(map[string]any)
	if ref["$ref"] != "#/components/responses/ServiceUnavailable" {
		t.Fatalf("POST /api/v0/iac/dead 503 = %#v, want the ServiceUnavailable reference", ref)
	}

	// #7548: the impact findings list answers the same retryable 503 for a
	// stale or timed-out guarded reader, so it must reference the shared
	// response that declares Retry-After.
	findings, _ := paths["/api/v0/supply-chain/impact/findings"].(map[string]any)
	get, _ := findings["get"].(map[string]any)
	findingsResponses, _ := get["responses"].(map[string]any)
	findingsRef, _ := findingsResponses["503"].(map[string]any)
	if findingsRef["$ref"] != "#/components/responses/ServiceUnavailable" {
		t.Fatalf("GET /api/v0/supply-chain/impact/findings 503 = %#v, want the ServiceUnavailable reference", findingsRef)
	}
}
