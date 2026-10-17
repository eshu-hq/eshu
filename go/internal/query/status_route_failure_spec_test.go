// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// statusSpecRoute is one #7674 slice 9 route operation in the published spec
// and the capability-matrix key its reader-verdict 503 names (#8227).
type statusSpecRoute struct {
	path, method, capability string
}

// statusSliceSpecRoutes lists every route whose read failure #7674 slice 9
// moved onto a fixed message behind the reader-fence 503, including the two
// admin audit reads in admin/identity.
var statusSliceSpecRoutes = []statusSpecRoute{
	{path: "/api/v0/status/pipeline", method: "get", capability: "operations.pipeline_status"},
	{path: "/api/v0/status/collectors", method: "get", capability: "operations.collector_status"},
	{path: "/api/v0/collectors", method: "get", capability: "operations.collector_status"},
	{path: "/api/v0/status/ingesters/{ingester}", method: "get", capability: "operations.ingester_status"},
	{path: "/api/v0/ingesters/{ingester}", method: "get", capability: "operations.ingester_status"},
	{path: "/api/v0/status/index", method: "get", capability: "operations.index_status"},
	{path: "/api/v0/index-status", method: "get", capability: "operations.index_status"},
	{path: "/api/v0/status/hosted-readiness", method: "get", capability: "operations.hosted_readiness_status"},
	{path: "/api/v0/status/freshness-causality", method: "get", capability: "operations.freshness_causality_status"},
	{path: "/api/v0/status/collector-readiness", method: "get", capability: "operations.collector_readiness_status"},
	{path: "/api/v0/collector-readiness", method: "get", capability: "operations.collector_readiness_status"},
	{path: "/api/v0/evidence/bundle", method: "get", capability: "operations.evidence_bundle_live"},
	{path: "/api/v0/auth/admin/audit/events", method: "get", capability: "audit.events.list"},
	{path: "/api/v0/auth/admin/audit/summary", method: "get", capability: "audit.summary.read"},
	{path: "/api/v0/status/semantic-extraction", method: "get", capability: "semantic_extraction.status"},
	{path: "/api/v0/status/operations", method: "get", capability: "operations.status"},
	{path: "/api/v0/status/governance", method: "get", capability: "hosted_governance.status"},
	{path: "/api/v0/status/answer-narration", method: "get", capability: "answer_narration.status"},
	{path: "/api/v0/evidence/relationships/{resolved_id}", method: "get", capability: "relationship_evidence.drilldown"},
	{path: "/api/v0/evidence/citations", method: "post", capability: "evidence_citation.packet"},
	{path: "/api/v0/investigations/deployable-unit/packet", method: "get", capability: "admission_decisions.list"},
	{path: "/api/v0/investigations/drift/packet", method: "get", capability: "cloud_runtime_drift.readback.list"},
}

// readerFenceSentence is the clause every slice 9 operation description
// carries: what a 503 with Retry-After means on the route.
const readerFenceSentence = "A 503 with Retry-After means a transient condition that is safe to retry"

// TestOpenAPIStatusSliceRoutesDeclareTheirFailureStatuses pins the #7674
// slice 9 failure contract in the published spec. Every route declares 500
// and the shared 503 (a stale or timed-out PostgreSQL reader now answers the
// retryable 503 on each of them; six routes declared no 503 before). Each
// operation description says what a 503 with and without Retry-After means
// (or that the route has no configuration 503) and names the capability its
// reader-verdict envelope carries (#8227). 499 is never declared.
func TestOpenAPIStatusSliceRoutesDeclareTheirFailureStatuses(t *testing.T) {
	t.Parallel()

	paths := adminSpecPaths(t)
	for _, route := range statusSliceSpecRoutes {
		op := testutil.MustMapField(t, testutil.MustMapField(t, paths, route.path), route.method)
		responses := testutil.MustMapField(t, op, "responses")
		for _, status := range []string{"500", "503"} {
			if _, ok := responses[status]; !ok {
				t.Errorf("%s %s: no %s declared", route.method, route.path, status)
			}
		}
		if ref, _ := testutil.MustMapField(t, responses, "503")["$ref"].(string); ref != "#/components/responses/ServiceUnavailable" {
			t.Errorf("%s %s: 503 = %v, want the shared ServiceUnavailable response", route.method, route.path, responses["503"])
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
		wantCapability := "names capability " + route.capability
		if !strings.Contains(description, wantCapability) {
			t.Errorf("%s %s: description lacks %q: %q", route.method, route.path, wantCapability, description)
		}
	}
}

// TestOpenAPIIndexStatusDeclares504 pins the 504 both index-status paths now
// answer: a scoped caller's repository count is a graph read, and a graph read
// deadline answers 504 through WriteGraphReadError (#7674 slice 9).
func TestOpenAPIIndexStatusDeclares504(t *testing.T) {
	t.Parallel()

	paths := adminSpecPaths(t)
	for _, path := range []string{"/api/v0/status/index", "/api/v0/index-status"} {
		responses := testutil.MustMapField(t, testutil.MustMapField(t, testutil.MustMapField(t, paths, path), "get"), "responses")
		if ref, _ := testutil.MustMapField(t, responses, "504")["$ref"].(string); ref != "#/components/responses/GatewayTimeout" {
			t.Errorf("%s: 504 = %v, want the shared GatewayTimeout response", path, responses["504"])
		}
	}
}

// TestOpenAPIIncidentContextDescribesTheReaderFence503 pins the incident
// context 503 description: a stale or timed-out reader answers a retryable
// 503 with Retry-After on that route too (#7674 slice 9).
func TestOpenAPIIncidentContextDescribesTheReaderFence503(t *testing.T) {
	t.Parallel()

	paths := adminSpecPaths(t)
	op := testutil.MustMapField(t, testutil.MustMapField(t, paths, "/api/v0/incidents/{incident_id}/context"), "get")
	description, _ := testutil.MustMapField(t, testutil.MustMapField(t, op, "responses"), "503")["description"].(string)
	if !strings.Contains(description, "Retry-After") || !strings.Contains(description, "stale or timed-out PostgreSQL reader") {
		t.Errorf("incident context 503 description = %q, want the reader-fence sentence with Retry-After", description)
	}
}
