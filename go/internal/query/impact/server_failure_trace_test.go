// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// traceFailureWorkload is the resolved service context the deployment-trace
// steps read from. Its READS_CONFIG_FROM artifact gives the config-derived
// cloud resource step an anchor to query.
func traceFailureWorkload() map[string]any {
	return map[string]any{
		"id": "workload:orders-api", "name": "orders-api", "kind": "service",
		"repo_id": "repo-orders", "repo_name": "orders-api",
		"deployment_evidence": map[string]any{"artifacts": []any{
			map[string]any{"relationship_type": "READS_CONFIG_FROM", "matched_value": "orders-config"},
		}},
	}
}

// traceFailureK8sEntity is a K8sResource whose container image sends the
// trace into the OCI registry-truth step.
func traceFailureK8sEntity() []querycontract.EntityContent {
	return []querycontract.EntityContent{{
		EntityID: "k8s-orders", RepoID: "repo-orders", RelativePath: "deploy/orders.yaml",
		EntityType: "K8sResource", EntityName: "orders-api",
		Metadata: map[string]any{
			"kind": "Deployment", "qualified_name": "production/Deployment/orders-api",
			"container_images": []any{"ghcr.io/acme/orders:latest"},
		},
	}}
}

// TestImpactTraceDeploymentStepsAnswerFixedServerFailures is the #7674
// regression for every read step of the deployment-trace route. Each answered
// 500 with "query <step>: <backend error>"; the k8s and gitops steps also
// skipped the reader-fence 503. Not parallel: it swaps queryHandlerTracer.
func TestImpactTraceDeploymentStepsAnswerFixedServerFailures(t *testing.T) {
	const path = "/api/v0/impact/trace-deployment-chain"
	const body = `{"service_name":"orders-api"}`
	graphStep := func(fail func(string) bool) func(error) *Handler {
		return func(err error) *Handler {
			return &Handler{
				Neo4j:        failingImpactGraph{err: err, fail: fail},
				Content:      failingImpactContent{},
				TraceContext: stubImpactTraceContext{workload: traceFailureWorkload()},
				Profile:      querycontract.ProfileProduction,
			}
		}
	}
	contentStep := func(content func(error) failingImpactContent, fail func(string) bool) func(error) *Handler {
		return func(err error) *Handler {
			return &Handler{
				Neo4j:        failingImpactGraph{err: err, fail: fail},
				Content:      content(err),
				TraceContext: stubImpactTraceContext{workload: traceFailureWorkload()},
				Profile:      querycontract.ProfileProduction,
			}
		}
	}
	never := func(string) bool { return false }

	runImpactFailureRoutes(t, []impactFailureRoute{
		{
			name: "service context", path: path, body: body, message: traceDeploymentQueryFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{TraceContext: stubImpactTraceContext{err: err}, Profile: querycontract.ProfileProduction}
			},
		},
		{name: "deployment sources", path: path, body: body, message: traceDeploymentSourcesFailedMessage, handler: graphStep(nil)},
		{
			name: "cloud resources", path: path, body: body, message: traceDeploymentCloudResourcesFailedMessage,
			handler: graphStep(cypherContains("-[rel:USES]->(c:CloudResource)")),
		},
		{
			name: "config-derived cloud resources", path: path, body: body, message: traceDeploymentConfigCloudResourcesFailedMessage,
			handler: graphStep(cypherContains("$config_anchor_pattern")),
		},
		{
			name: "uncorrelated cloud resources", path: path, body: body, message: traceDeploymentUncorrelatedCloudResourcesFailedMessage,
			handler: graphStep(cypherContains("MATCH (n:CloudResource)")),
		},
		{
			name: "k8s resources", path: path, body: body, message: traceDeploymentK8sResourcesFailedMessage,
			handler: contentStep(func(err error) failingImpactContent { return failingImpactContent{searchErr: err} }, never),
		},
		{
			name: "gitops evidence", path: path, body: body, message: traceDeploymentGitOpsEvidenceFailedMessage,
			handler: contentStep(func(err error) failingImpactContent { return failingImpactContent{listErr: err} }, never),
		},
		{
			name: "oci registry truth", path: path, body: body, message: traceDeploymentOCIRegistryTruthFailedMessage,
			handler: contentStep(func(error) failingImpactContent { return failingImpactContent{search: traceFailureK8sEntity()} },
				cypherContains("ContainerImageTagObservation")),
		},
	})
}

// TestImpactDeploymentConfigInfluenceAnswersFixedServerFailures covers the
// service-context read ("query failed: <err>") and the enrichment reads.
func TestImpactDeploymentConfigInfluenceAnswersFixedServerFailures(t *testing.T) {
	const path = "/api/v0/impact/deployment-config-influence"
	const body = `{"service_name":"orders-api"}`
	runImpactFailureRoutes(t, []impactFailureRoute{
		{
			name: "service context", path: path, body: body, message: deploymentConfigInfluenceQueryFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{TraceContext: stubImpactTraceContext{err: err}, Profile: querycontract.ProfileProduction}
			},
		},
		{
			name: "enrichment", path: path, body: body, message: deploymentConfigInfluenceEnrichmentFailedMessage,
			handler: func(err error) *Handler {
				return &Handler{
					Neo4j:        failingImpactGraph{err: err},
					Content:      failingImpactContent{},
					TraceContext: stubImpactTraceContext{workload: traceFailureWorkload()},
					Profile:      querycontract.ProfileProduction,
				}
			},
		},
	})
}

// TestImpactChangeSurfaceRoutesAnswerFixedFailures covers the change-surface
// investigation, pre-change, and developer change plan routes. A code-evidence
// (content store) failure keeps its 503 but answers a fixed message; it
// answered 503 with the store's error text. A graph failure answers 500.
func TestImpactChangeSurfaceRoutesAnswerFixedFailures(t *testing.T) {
	codeSurfaceFails := func(err error) *Handler {
		return &Handler{CodeSurface: stubImpactCodeSurface{err: err}, Profile: querycontract.ProfileProduction}
	}
	graphFails := func(fail func(string) bool) func(error) *Handler {
		return func(err error) *Handler {
			return &Handler{
				Neo4j: failingImpactGraph{err: err, fail: fail, answer: func(string, map[string]any) []map[string]any {
					return []map[string]any{{"id": "workload:orders-api", "name": "orders", "labels": []any{"Workload"}, "repo_id": "repo-api"}}
				}},
				CodeSurface: stubImpactCodeSurface{},
				Profile:     querycontract.ProfileProduction,
			}
		}
	}
	const investigate = "/api/v0/impact/change-surface/investigate"
	const investigateBody = `{"target":"workload:orders-api","target_type":"service"}`
	const preChangeBody = `{"target":"workload:orders-api","target_type":"service"}`
	runImpactFailureRoutes(t, []impactFailureRoute{
		{
			name: "investigation code evidence", path: investigate, body: investigateBody,
			message: changeSurfaceInvestigationCodeEvidenceFailedMessage, faultStatus: http.StatusServiceUnavailable,
			handler: codeSurfaceFails,
		},
		{
			name: "investigation target", path: investigate, body: investigateBody,
			message: changeSurfaceInvestigationTargetFailedMessage, handler: graphFails(nil),
		},
		{
			name: "investigation traversal", path: investigate, body: investigateBody,
			message: changeSurfaceInvestigationTraversalFailedMessage, handler: graphFails(cypherContains("MATCH path =")),
		},
		{
			name: "pre-change code evidence", path: "/api/v0/impact/pre-change", body: preChangeBody,
			message: preChangeImpactCodeEvidenceFailedMessage, faultStatus: http.StatusServiceUnavailable,
			handler: codeSurfaceFails,
		},
		{
			name: "pre-change graph", path: "/api/v0/impact/pre-change", body: preChangeBody,
			message: preChangeImpactQueryFailedMessage, handler: graphFails(nil),
		},
		{
			name: "developer plan code evidence", path: "/api/v0/impact/developer-change-plan", body: preChangeBody,
			message: developerChangePlanCodeEvidenceFailedMessage, faultStatus: http.StatusServiceUnavailable,
			handler: codeSurfaceFails,
		},
		{
			name: "developer plan graph", path: "/api/v0/impact/developer-change-plan", body: preChangeBody,
			message: developerChangePlanQueryFailedMessage, handler: graphFails(nil),
		},
	})
}

// TestImpactChangeSurfaceNotGrantedKeepsNotFound pins the not-granted answer
// each code-evidence route already gave: 404, with fixed text.
func TestImpactChangeSurfaceNotGrantedKeepsNotFound(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]string{
		"/api/v0/impact/change-surface/investigate": "repository not found",
		"/api/v0/impact/pre-change":                 ErrChangeSurfaceRepoNotGranted.Error(),
		"/api/v0/impact/developer-change-plan":      ErrChangeSurfaceRepoNotGranted.Error(),
	} {
		handler := &Handler{CodeSurface: stubImpactCodeSurface{err: ErrChangeSurfaceRepoNotGranted}, Profile: querycontract.ProfileProduction}
		rec := serveImpactRequest(t, handler, path, `{"target":"workload:orders-api","target_type":"service"}`)
		if rec.Code != http.StatusNotFound || !containsDetail(rec.Body.Bytes(), want) {
			t.Fatalf("%s: status = %d, body = %s; want 404 with %q", path, rec.Code, rec.Body.String(), want)
		}
	}
}
