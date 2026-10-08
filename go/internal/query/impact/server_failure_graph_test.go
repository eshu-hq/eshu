// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestImpactGraphRoutesAnswerFixedServerFailures is the #7674 regression for
// the graph-backed impact routes. Each failing step answered 500 with the
// backend error text in the body. It must answer its fixed message with the
// error on the span, 499 for a client cancel, and the retryable 503 for a
// stale reader. Not parallel: it swaps queryHandlerTracer.
func TestImpactGraphRoutesAnswerFixedServerFailures(t *testing.T) {
	graphOnly := func(fail func(string) bool, answer func(string, map[string]any) []map[string]any) func(error) *Handler {
		return func(err error) *Handler {
			return &Handler{Neo4j: failingImpactGraph{err: err, fail: fail, answer: answer}, Profile: querycontract.ProfileProduction}
		}
	}
	workloadAnchor := func(string, map[string]any) []map[string]any {
		return []map[string]any{{"id": "workload:orders-api", "name": "orders", "labels": []any{"Workload"}, "repo_id": "repo-api"}}
	}
	entityMapAnchor := func(cypher string, _ map[string]any) []map[string]any {
		return []map[string]any{{
			"id": "workload:checkout", "name": "checkout", "labels": []any{"Workload"}, "repo_id": "repo-checkout",
			"anchor_label": "Workload", "anchor_property": "id", "anchor_value": "workload:checkout",
		}}
	}
	resourceAnchor := func(string, map[string]any) []map[string]any {
		return []map[string]any{{"id": "cloud:queue:orders", "name": "orders", "labels": []any{"CloudResource"}, "environment": "prod"}}
	}
	pathAnchor := func(_ string, params map[string]any) []map[string]any {
		if _, ok := params["target_id"]; ok {
			return []map[string]any{{"label": "Repository", "id": "repo:api", "name": "api", "labels": []any{"Repository"}}}
		}
		return []map[string]any{{"label": "CloudResource", "id": "resource:queue", "name": "queue", "labels": []any{"CloudResource"}}}
	}
	notResolver := func(cypher string) bool { return !strings.Contains(cypher, "MATCH (n:") }

	runImpactFailureRoutes(t, []impactFailureRoute{
		{
			name: "blast radius", path: "/api/v0/impact/blast-radius",
			body:    `{"target":"payments-core","target_type":"repository"}`,
			message: blastRadiusQueryFailedMessage, handler: graphOnly(nil, nil),
		},
		{
			name: "legacy change surface target", path: "/api/v0/impact/change-surface",
			body:    `{"kind":"service","target":"orders"}`,
			message: changeSurfaceLegacyTargetFailedMessage, handler: graphOnly(nil, nil),
		},
		{
			name: "legacy change surface traversal", path: "/api/v0/impact/change-surface",
			body:    `{"kind":"service","target":"orders"}`,
			message: changeSurfaceLegacyTraversalFailedMessage, handler: graphOnly(cypherContains("MATCH path ="), workloadAnchor),
		},
		{
			name: "entity map start", path: "/api/v0/impact/entity-map",
			body:    `{"from":"checkout","from_type":"service"}`,
			message: entityMapStartFailedMessage, handler: graphOnly(nil, nil),
		},
		{
			name: "entity map traversal", path: "/api/v0/impact/entity-map",
			body:    `{"from":"checkout","from_type":"service"}`,
			message: entityMapTraversalFailedMessage, handler: graphOnly(notResolver, entityMapAnchor),
		},
		{
			name: "resource investigation target", path: "/api/v0/impact/resource-investigation",
			body:    `{"query":"orders","resource_type":"queue","limit":1}`,
			message: resourceInvestigationTargetFailedMessage, handler: graphOnly(nil, nil),
		},
		{
			name: "resource investigation sections", path: "/api/v0/impact/resource-investigation",
			body:    `{"query":"orders","resource_type":"queue","limit":1}`,
			message: resourceInvestigationSectionsFailedMessage, handler: graphOnly(notResolver, resourceAnchor),
		},
		{
			name: "resource to code start", path: "/api/v0/impact/trace-resource-to-code",
			body:    `{"start":"resource:queue"}`,
			message: resourceToCodeStartFailedMessage, handler: graphOnly(nil, nil),
		},
		{
			name: "resource to code paths", path: "/api/v0/impact/trace-resource-to-code",
			body:    `{"start":"resource:queue"}`,
			message: resourceToCodePathsFailedMessage, handler: graphOnly(cypherContains("MATCH path ="), pathAnchor),
		},
		{
			name: "dependency path endpoints", path: "/api/v0/impact/explain-dependency-path",
			body:    `{"source":"resource:queue","target":"repo:api"}`,
			message: dependencyPathEndpointsFailedMessage, handler: graphOnly(nil, nil),
		},
		{
			name: "dependency path query", path: "/api/v0/impact/explain-dependency-path",
			body:    `{"source":"resource:queue","target":"repo:api"}`,
			message: dependencyPathQueryFailedMessage, handler: graphOnly(cypherContains("shortestPath"), pathAnchor),
		},
		{
			name: "contract impact", path: "/api/v0/impact/contracts",
			body:    `{"family":"http","provider_repo_id":"repo-api"}`,
			message: contractImpactQueryFailedMessage, handler: graphOnly(nil, nil),
		},
	})
}

// TestImpactExposurePathAnswersFixedServerFailures covers the exposure-path
// source read, which answered 400 with the content store's error text, and
// its traversal, which answered 500 with the graph's.
func TestImpactExposurePathAnswersFixedServerFailures(t *testing.T) {
	source := &querycontract.EntityContent{
		EntityID: "fn-a", RepoID: "repo-a", EntityName: "handle", EntityType: "function",
		Metadata: map[string]any{"dead_code_root_kinds": []any{"go.net_http_handler_signature"}},
	}
	runImpactFailureRoutes(t, []impactFailureRoute{
		{
			name: "exposure path source", path: "/api/v0/impact/trace-exposure-path",
			body: `{"source_entity_id":"fn-a"}`, message: exposurePathSourceFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{Content: failingImpactContent{getErr: err}, Profile: querycontract.ProfileProduction}
			},
		},
		{
			name: "exposure path traversal", path: "/api/v0/impact/trace-exposure-path",
			body: `{"source_entity_id":"fn-a"}`, message: exposurePathTraversalFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{
					Neo4j:   failingImpactGraph{err: err},
					Content: failingImpactContent{entity: source},
					Profile: querycontract.ProfileProduction,
				}
			},
		},
	})
}

// TestImpactExposurePathAmbiguousSourceKeepsBadRequest pins the one source
// error the exposure-path 400 still echoes: a source name that matched more
// than one entity, whose text names only the caller's selector.
func TestImpactExposurePathAmbiguousSourceKeepsBadRequest(t *testing.T) {
	t.Parallel()

	matches := []querycontract.EntityContent{
		{EntityID: "fn-a", RepoID: "repo-a", EntityName: "handle", EntityType: "function", RelativePath: "a/handler.go"},
		{EntityID: "fn-b", RepoID: "repo-a", EntityName: "handle", EntityType: "function", RelativePath: "b/handler.go"},
	}
	handler := &Handler{Content: failingImpactContent{search: matches}, Profile: querycontract.ProfileProduction}
	rec := serveImpactRequest(t, handler, "/api/v0/impact/trace-exposure-path", `{"source":"handle","repo_id":"repo-a"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "matched multiple entities") {
		t.Fatalf("status = %d, body = %s; want 400 naming the ambiguous source", rec.Code, rec.Body.String())
	}
}

// TestImpactContractGraphUnavailableKeepsSentinelText pins the contract
// route's own 503: it answers the sentinel's fixed text, never a wrapped
// error.
func TestImpactContractGraphUnavailableKeepsSentinelText(t *testing.T) {
	t.Parallel()

	rec := serveImpactRequest(t, &Handler{Profile: querycontract.ProfileProduction},
		"/api/v0/impact/contracts", `{"family":"http","provider_repo_id":"repo-api"}`)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), errContractImpactGraphUnavailable.Error()) {
		t.Fatalf("status = %d, body = %s; want 503 with %q", rec.Code, rec.Body.String(), errContractImpactGraphUnavailable.Error())
	}
}
