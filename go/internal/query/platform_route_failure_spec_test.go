// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// infraSpecRoute is one #7674 slice 10 route operation in the published spec,
// the capability its graph or reader verdict names (empty: no
// capability-matrix key yet, #8227), and whether a graph read deadline
// answers 504 on it.
type infraSpecRoute struct {
	path, method, capability string
	graphDeadline            bool
}

// infraSliceSpecRoutes lists every route whose read failure #7674 slice 10
// moved onto a fixed message behind WriteGraphReadError.
var infraSliceSpecRoutes = []infraSpecRoute{
	{path: "/api/v0/infra/resources/search", method: "post", capability: "platform_impact.deployment_chain", graphDeadline: true},
	{path: "/api/v0/infra/relationships", method: "post", capability: "platform_impact.deployment_chain", graphDeadline: true},
	{path: "/api/v0/ecosystem/overview", method: "get", capability: "platform_impact.context_overview", graphDeadline: true},
	{path: "/api/v0/ecosystem/graph-summary", method: "post", capability: "platform_impact.graph_summary_packet", graphDeadline: true},
	{path: "/api/v0/relationships/catalog", method: "post", capability: "platform_impact.relationships_catalog", graphDeadline: true},
	{path: "/api/v0/relationships/edges", method: "post", capability: "platform_impact.relationships_catalog", graphDeadline: true},
	{path: "/api/v0/infra/resources/count", method: "get", capability: "platform_impact.infra_resource_aggregate", graphDeadline: true},
	{path: "/api/v0/infra/resources/inventory", method: "get", capability: "platform_impact.infra_resource_aggregate", graphDeadline: true},
	{path: "/api/v0/cloud/resources", method: "get", capability: "platform_impact.cloud_resource_list", graphDeadline: true},
	{path: "/api/v0/graph/entities", method: "get", capability: "platform_impact.context_overview", graphDeadline: true},
	{path: "/api/v0/images", method: "get", capability: "platform_impact.container_image_list", graphDeadline: true},
	{path: "/api/v0/images/tag-history", method: "get", capability: "platform_impact.container_image_tag_history", graphDeadline: true},
	{path: "/api/v0/status/operator-control-plane", method: "get", capability: "operations.operator_control_plane_status"},
	{path: "/api/v0/evidence/admission-decisions", method: "get", capability: "admission_decisions.list"},
	{path: "/api/v0/cloud/runtime-drift/findings", method: "post", capability: "cloud_runtime_drift.readback.list"},
}

// TestOpenAPIInfraSliceRoutesDeclareTheirFailureStatuses pins the #7674
// slice 10 failure contract in the published spec. Every route declares 500
// and the shared 503 (POST /api/v0/cloud/runtime-drift/findings declared no
// 503 before, though a stale reader now answers one; GET
// /api/v0/cloud/resources and the two aggregate routes declared no 500), and
// every graph route declares the shared 504. Each operation description says
// what a 503 with and without Retry-After means and names the capability its
// verdict envelope carries, or says it carries none (#8227). 499 is never
// declared.
func TestOpenAPIInfraSliceRoutesDeclareTheirFailureStatuses(t *testing.T) {
	t.Parallel()

	paths := adminSpecPaths(t)
	for _, route := range infraSliceSpecRoutes {
		op := testutil.MustMapField(t, testutil.MustMapField(t, paths, route.path), route.method)
		responses := testutil.MustMapField(t, op, "responses")
		if _, ok := responses["500"]; !ok {
			t.Errorf("%s %s: no 500 declared", route.method, route.path)
		}
		if ref, _ := testutil.MustMapField(t, responses, "503")["$ref"].(string); ref != "#/components/responses/ServiceUnavailable" {
			t.Errorf("%s %s: 503 = %v, want the shared ServiceUnavailable response", route.method, route.path, responses["503"])
		}
		if route.graphDeadline {
			if ref, _ := testutil.MustMapField(t, responses, "504")["$ref"].(string); ref != "#/components/responses/GatewayTimeout" {
				t.Errorf("%s %s: 504 = %v, want the shared GatewayTimeout response", route.method, route.path, responses["504"])
			}
		}
		if _, ok := responses["499"]; ok {
			t.Errorf("%s %s: declares 499; a client cancel is never declared", route.method, route.path)
		}
		description, _ := op["description"].(string)
		explainsNoRetryAfter := strings.Contains(description, "a 503 without Retry-After means") ||
			strings.Contains(description, "every 503 it answers carries Retry-After")
		if !strings.Contains(description, readerFenceSentence) || !explainsNoRetryAfter {
			t.Errorf("%s %s: description does not say what a 503 with and without Retry-After means: %q", route.method, route.path, description)
		}
		if !strings.Contains(description, "No 5xx body carries backend error text.") {
			t.Errorf("%s %s: description does not say 5xx bodies carry no backend error text", route.method, route.path)
		}
		wantCapability := "carries no capability field (#8227)"
		if route.capability != "" {
			wantCapability = "names capability " + route.capability
		}
		if !strings.Contains(description, wantCapability) {
			t.Errorf("%s %s: description lacks %q: %q", route.method, route.path, wantCapability, description)
		}
	}
}

// TestOpenAPISurfaceInventoryDeclaresItsFailure pins GET
// /api/v0/surface-inventory: its only failure step, the embedded inventory
// load, answers 500 (499 is never declared).
func TestOpenAPISurfaceInventoryDeclaresItsFailure(t *testing.T) {
	t.Parallel()

	paths := adminSpecPaths(t)
	responses := testutil.MustMapField(t, testutil.MustMapField(t, testutil.MustMapField(t, paths, "/api/v0/surface-inventory"), "get"), "responses")
	if _, ok := responses["500"]; !ok {
		t.Errorf("surface inventory: no 500 declared")
	}
	if _, ok := responses["499"]; ok {
		t.Errorf("surface inventory: declares 499; a client cancel is never declared")
	}
}
