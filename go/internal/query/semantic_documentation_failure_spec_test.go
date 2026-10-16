// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// semanticDocumentationSpecRoutes lists every route #7674 slice 14 moved onto
// the reader fence and the server-failure helper, with the capability its
// reader-verdict 503 names. Only the two aggregate routes have a
// configuration 503 (an unconfigured aggregate store).
var semanticDocumentationSpecRoutes = []struct {
	statusSpecRoute
	configuration503 bool
}{
	{statusSpecRoute: statusSpecRoute{path: "/api/v0/semantic/documentation-observations", method: "get", capability: "semantic_evidence.documentation_observations.list"}},
	{statusSpecRoute: statusSpecRoute{path: "/api/v0/semantic/code-hints", method: "get", capability: "semantic_evidence.code_hints.list"}},
	{statusSpecRoute: statusSpecRoute{path: "/api/v0/documentation/findings", method: "get", capability: "documentation_findings.list"}},
	{statusSpecRoute: statusSpecRoute{path: "/api/v0/documentation/facts", method: "get", capability: "documentation_facts.list"}},
	{statusSpecRoute: statusSpecRoute{path: "/api/v0/documentation/findings/{finding_id}/evidence-packet", method: "get", capability: "documentation_evidence_packet.read"}},
	{statusSpecRoute: statusSpecRoute{path: "/api/v0/documentation/evidence-packets/{packet_id}/freshness", method: "get", capability: "documentation_evidence_packet.freshness"}},
	{statusSpecRoute: statusSpecRoute{path: "/api/v0/documentation/findings/count", method: "get", capability: "documentation_findings.aggregate"}, configuration503: true},
	{statusSpecRoute: statusSpecRoute{path: "/api/v0/documentation/findings/inventory", method: "get", capability: "documentation_findings.aggregate"}, configuration503: true},
}

// TestOpenAPISemanticAndDocumentationRoutesDeclareTheirFailureStatuses pins
// the #7674 slice 14 failure contract in the published spec. Every route
// declares 500 and the shared 503: a stale or timed-out PostgreSQL reader now
// answers the retryable 503 on each of them, and the two aggregate routes
// declared neither before. Each operation description says what a 503 with
// Retry-After means, says either what a 503 without it means (the aggregate
// routes) or that the route has no configuration 503, and names the
// capability its reader-verdict envelope carries. 499 is never declared.
func TestOpenAPISemanticAndDocumentationRoutesDeclareTheirFailureStatuses(t *testing.T) {
	t.Parallel()

	paths := adminSpecPaths(t)
	for _, route := range semanticDocumentationSpecRoutes {
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
		noRetryAfter := "every 503 it answers carries Retry-After"
		if route.configuration503 {
			noRetryAfter = "a 503 without Retry-After means"
		}
		for _, want := range []string{readerFenceSentence, noRetryAfter, "names capability " + route.capability} {
			if !strings.Contains(description, want) {
				t.Errorf("%s %s: description lacks %q: %q", route.method, route.path, want, description)
			}
		}
	}
}
