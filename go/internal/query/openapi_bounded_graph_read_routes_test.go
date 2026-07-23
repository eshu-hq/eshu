// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

// boundedGraphReadRoute is one HTTP route whose handler routes a graph-read
// failure through the bounded-availability contract (WriteGraphReadError or
// graphReadErrorEnvelope), and which must therefore advertise 503 and 504 in
// the OpenAPI spec.
type boundedGraphReadRoute struct {
	method string
	path   string
}

// boundedGraphReadRoutes is the full set of guarded routes. #5273 extended the
// bounded graph-read mapping across the graph-backed query surface; without
// this list the spec silently drifts, because scripts/verify-openapi.sh only
// checks that a route HAS an entry, never that the entry documents every
// status the handler can return.
//
// Derive this list TRANSITIVELY, not by looking only at the registered handler
// function. A route qualifies when the guard sits anywhere in the call graph
// reachable from its handler: POST /api/v0/code/relationships/story is guarded
// inside handleRepoScopedOverrideStory, and GET /api/v0/services/{service_name}/story
// inside BuildServiceStoryEnvelope, neither of which is the function registered
// with the mux. An enclosing-function-only sweep misses exactly those cases.
//
// Two known exclusions:
//   - POST /api/v0/code/language-query is guarded nowhere: the error envelope
//     carries a capability and that route has none in the capability catalog,
//     so rather than invent one it still returns 500
//     (see docs/public/reference/telemetry/graph-read-safety.md).
//   - POST /api/v0/code/visualize IS guarded but has no OpenAPI path entry at
//     all, predating this change; it cannot carry response codes until that
//     entry exists.
var boundedGraphReadRoutes = []boundedGraphReadRoute{
	{method: "get", path: "/api/v0/catalog"},
	{method: "get", path: "/api/v0/cloud/resources"},
	{method: "get", path: "/api/v0/codeowners/ownership"},
	{method: "get", path: "/api/v0/dependencies"},
	{method: "get", path: "/api/v0/ecosystem/overview"},
	{method: "get", path: "/api/v0/entities/{entity_id}/context"},
	{method: "get", path: "/api/v0/graph/entities"},
	{method: "get", path: "/api/v0/iac/resources"},
	{method: "get", path: "/api/v0/images"},
	{method: "get", path: "/api/v0/images/tag-history"},
	{method: "get", path: "/api/v0/infra/resources/count"},
	{method: "get", path: "/api/v0/infra/resources/inventory"},
	{method: "get", path: "/api/v0/investigations/services/{service_name}"},
	{method: "get", path: "/api/v0/investigations/supply-chain/impact/packet"},
	{method: "get", path: "/api/v0/package-registry/correlations"},
	{method: "get", path: "/api/v0/package-registry/dependencies"},
	{method: "get", path: "/api/v0/package-registry/dependency-chains"},
	{method: "get", path: "/api/v0/package-registry/packages"},
	{method: "get", path: "/api/v0/package-registry/packages/count"},
	{method: "get", path: "/api/v0/package-registry/packages/inventory"},
	{method: "get", path: "/api/v0/package-registry/versions"},
	{method: "get", path: "/api/v0/repositories"},
	{method: "get", path: "/api/v0/repositories/{repo_id}/branches"},
	{method: "get", path: "/api/v0/repositories/{repo_id}/content"},
	{method: "get", path: "/api/v0/repositories/{repo_id}/context"},
	{method: "get", path: "/api/v0/repositories/{repo_id}/coverage"},
	{method: "get", path: "/api/v0/repositories/{repo_id}/freshness"},
	{method: "get", path: "/api/v0/repositories/{repo_id}/stats"},
	{method: "get", path: "/api/v0/repositories/{repo_id}/story"},
	{method: "get", path: "/api/v0/repositories/{repo_id}/tree"},
	{method: "get", path: "/api/v0/secrets-iam/posture-summary"},
	{method: "get", path: "/api/v0/services/{service_name}/context"},
	{method: "get", path: "/api/v0/services/{service_name}/story"},
	{method: "get", path: "/api/v0/supply-chain/advisories/evidence"},
	{method: "get", path: "/api/v0/supply-chain/container-images/identities"},
	{method: "get", path: "/api/v0/supply-chain/container-images/identities/count"},
	{method: "get", path: "/api/v0/supply-chain/container-images/identities/inventory"},
	{method: "get", path: "/api/v0/supply-chain/impact/explain"},
	{method: "get", path: "/api/v0/supply-chain/impact/findings"},
	{method: "get", path: "/api/v0/supply-chain/impact/findings/count"},
	{method: "get", path: "/api/v0/supply-chain/impact/inventory"},
	{method: "get", path: "/api/v0/supply-chain/sbom-attestations/attachments"},
	{method: "get", path: "/api/v0/supply-chain/sbom-attestations/attachments/count"},
	{method: "get", path: "/api/v0/supply-chain/sbom-attestations/attachments/inventory"},
	{method: "get", path: "/api/v0/supply-chain/security-alerts/reconciliations"},
	{method: "get", path: "/api/v0/supply-chain/security-alerts/reconciliations/count"},
	{method: "get", path: "/api/v0/supply-chain/security-alerts/reconciliations/inventory"},
	{method: "get", path: "/api/v0/workloads/{workload_id}/context"},
	{method: "get", path: "/api/v0/workloads/{workload_id}/story"},
	{method: "post", path: "/api/v0/code/bundles"},
	{method: "post", path: "/api/v0/code/call-chain"},
	{method: "post", path: "/api/v0/code/call-graph/metrics"},
	{method: "post", path: "/api/v0/code/complexity"},
	{method: "post", path: "/api/v0/code/cypher"},
	{method: "post", path: "/api/v0/code/dead-code"},
	{method: "post", path: "/api/v0/code/dead-code/cross-repo"},
	{method: "post", path: "/api/v0/code/dead-code/investigate"},
	{method: "post", path: "/api/v0/code/flow/cfg-summary"},
	{method: "post", path: "/api/v0/code/flow/pdg-summary"},
	{method: "post", path: "/api/v0/code/flow/reaching-def"},
	{method: "post", path: "/api/v0/code/flow/taint-path"},
	{method: "post", path: "/api/v0/code/imports/investigate"},
	{method: "post", path: "/api/v0/code/quality/inspect"},
	{method: "post", path: "/api/v0/code/relationships"},
	{method: "post", path: "/api/v0/code/relationships/story"},
	{method: "post", path: "/api/v0/code/routes/callers"},
	{method: "post", path: "/api/v0/code/search"},
	{method: "post", path: "/api/v0/code/security/secrets/investigate"},
	{method: "post", path: "/api/v0/code/structure/inventory"},
	{method: "post", path: "/api/v0/code/symbols/search"},
	{method: "post", path: "/api/v0/code/topics/investigate"},
	{method: "post", path: "/api/v0/compare/environments"},
	{method: "post", path: "/api/v0/ecosystem/graph-summary"},
	{method: "post", path: "/api/v0/entities/resolve"},
	{method: "post", path: "/api/v0/impact/blast-radius"},
	{method: "post", path: "/api/v0/impact/change-surface"},
	{method: "post", path: "/api/v0/impact/change-surface/investigate"},
	{method: "post", path: "/api/v0/impact/contracts"},
	{method: "post", path: "/api/v0/impact/deployment-config-influence"},
	{method: "post", path: "/api/v0/impact/developer-change-plan"},
	{method: "post", path: "/api/v0/impact/entity-map"},
	{method: "post", path: "/api/v0/impact/explain-dependency-path"},
	{method: "post", path: "/api/v0/impact/pre-change"},
	{method: "post", path: "/api/v0/impact/resource-investigation"},
	{method: "post", path: "/api/v0/impact/trace-deployment-chain"},
	{method: "post", path: "/api/v0/impact/trace-exposure-path"},
	{method: "post", path: "/api/v0/impact/trace-resource-to-code"},
	{method: "post", path: "/api/v0/infra/relationships"},
	{method: "post", path: "/api/v0/infra/resources/search"},
	{method: "post", path: "/api/v0/relationships/catalog"},
	{method: "post", path: "/api/v0/relationships/edges"},
}

// TestOpenAPIDocumentsBoundedGraphReadFailuresOnEveryGuardedRoute asserts every
// guarded route advertises both bounded-availability statuses. A handler that
// gains the guard without updating its OpenAPI entry leaves codegen clients
// unaware the route can return 503/504.
func TestOpenAPIDocumentsBoundedGraphReadFailuresOnEveryGuardedRoute(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("OpenAPI spec has no paths object")
	}

	for _, route := range boundedGraphReadRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			t.Parallel()
			path, ok := paths[route.path].(map[string]any)
			if !ok {
				t.Fatalf("OpenAPI spec has no path entry for %s", route.path)
			}
			operation, ok := path[route.method].(map[string]any)
			if !ok {
				t.Fatalf("OpenAPI path %s has no %s operation", route.path, route.method)
			}
			responses, ok := operation["responses"].(map[string]any)
			if !ok {
				t.Fatalf("OpenAPI operation %s %s has no responses", route.method, route.path)
			}
			for _, status := range []string{"503", "504"} {
				if _, ok := responses[status]; !ok {
					t.Errorf("%s %s does not document %s; its handler routes graph-read failures through the bounded-availability contract",
						route.method, route.path, status)
				}
			}
		})
	}
}
