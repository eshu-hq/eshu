// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// ociLimitsHandlerContentStore is a minimal k8s content store carrying one
// K8sResource entity whose container_images names one OCI tag ref, so
// TraceDeploymentChain reaches h.FetchOCIImageRegistryTruth. Modeled on
// recordingK8sContentStore (trace_deployment_k8s_limits_test.go).
type ociLimitsHandlerContentStore struct {
	content.FakePortContentStore
	imageRef string
}

func (s ociLimitsHandlerContentStore) SearchEntitiesByName(
	context.Context, string, string, string, int,
) ([]querycontract.EntityContent, error) {
	return []querycontract.EntityContent{{
		EntityID:     "k8s-payments",
		RepoID:       "repo-orders",
		RelativePath: "deploy/payments.yaml",
		EntityType:   "K8sResource",
		EntityName:   "orders-api",
		Metadata: map[string]any{
			"kind":             "Deployment",
			"qualified_name":   "production/Deployment/payments",
			"container_images": []any{s.imageRef},
		},
	}}, nil
}

// ociLimitsHandlerReader wires the workload/repository lookups
// TraceDeploymentChain needs (mirroring
// TestTraceDeploymentChainClampsAbsurdMaxDepthInsteadOfRejecting) plus the
// bounded OCI tag-observation statement from tagRows.
func ociLimitsHandlerReader(t *testing.T, tagRows map[string][]map[string]any) graph.FakeWorkloadGraphReader {
	t.Helper()
	workload := map[string]any{
		"id":        "workload:orders-api",
		"instances": []any{},
		"kind":      "service",
		"name":      "orders-api",
		"repo_id":   "repo-orders",
		"repo_name": "orders-api",
	}
	bounded := graph.OCIBoundedFakeReader{
		T: t,
		Statements: []graph.OCIBoundedStatementFixture{
			{
				CypherContains: "MATCH (tag:ContainerImageTagObservation)",
				KeyParam:       "image_refs",
				KeyField:       "image_ref",
				RowsByKey:      tagRows,
			},
		},
	}
	return graph.FakeWorkloadGraphReader{
		RunSingleByMatch: map[string]map[string]any{
			"w.name = $service_name": workload,
			"w.id = $workload_id":    workload,
		},
		RunFn: func(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "MATCH (tag:ContainerImageTagObservation)"):
				return bounded.Run(ctx, cypher, params)
			case strings.Contains(cypher, "w.name = $service_name"):
				return []map[string]any{workload}, nil
			case strings.Contains(cypher, "DEFINES]-(r:Repository)"):
				return []map[string]any{{"repo_id": "repo-orders", "repo_name": "orders-api"}}, nil
			default:
				return nil, nil
			}
		},
	}
}

// TestTraceDeploymentChainDisclosesImageRegistryTruthLimits is decision test
// 5 (#6590): the trace_deployment_chain response carries
// image_registry_truth_limits with image_registry_truth_complete=true and no
// withheld refs in the 1-observation case, and complete=false plus the
// withheld ref and reason when the tag-observation read overflows; either
// way, deployment_overview.image_registry_match_count only counts rows that
// actually resolved.
func TestTraceDeploymentChainDisclosesImageRegistryTruthLimits(t *testing.T) {
	t.Parallel()

	const imageRef = "ghcr.io/acme/c:latest"
	digest := "sha256:" + strings.Repeat("1", 64)

	t.Run("complete", func(t *testing.T) {
		t.Parallel()
		handler := &Handler{
			Neo4j:   ociLimitsHandlerReader(t, map[string][]map[string]any{imageRef: {ociTagRow(imageRef, digest, "repo:c")}}),
			Content: ociLimitsHandlerContentStore{imageRef: imageRef},
		}
		body := httptestTraceDeploymentChain(t, handler)

		limits := querycontract.MapValue(body, "image_registry_truth_limits")
		if got, want := querycontract.BoolVal(limits, "image_registry_truth_complete"), true; got != want {
			t.Fatalf("image_registry_truth_limits.image_registry_truth_complete = %v, want %v: %#v", got, want, limits)
		}
		if got, want := querycontract.IntVal(limits, "truncated_image_ref_count"), 0; got != want {
			t.Fatalf("image_registry_truth_limits.truncated_image_ref_count = %d, want %d", got, want)
		}
	})

	t.Run("overflow", func(t *testing.T) {
		t.Parallel()
		handler := &Handler{
			Neo4j:   ociLimitsHandlerReader(t, map[string][]map[string]any{imageRef: repeatOCITagRow(imageRef, digest, "repo:c", 750)}),
			Content: ociLimitsHandlerContentStore{imageRef: imageRef},
		}
		body := httptestTraceDeploymentChain(t, handler)

		limits := querycontract.MapValue(body, "image_registry_truth_limits")
		if got, want := querycontract.BoolVal(limits, "image_registry_truth_complete"), false; got != want {
			t.Fatalf("image_registry_truth_limits.image_registry_truth_complete = %v, want %v: %#v", got, want, limits)
		}
		if got, want := querycontract.StringVal(limits, "image_registry_truth_incomplete_reason"), ociRegistryTruthRowLimitReason; got != want {
			t.Fatalf("image_registry_truth_limits.image_registry_truth_incomplete_reason = %q, want %q", got, want)
		}
		refs := querycontract.StringSliceVal(limits, "truncated_image_refs")
		if len(refs) != 1 || refs[0] != imageRef {
			t.Fatalf("image_registry_truth_limits.truncated_image_refs = %#v, want [%q]", refs, imageRef)
		}
		overview := querycontract.MapValue(body, "deployment_overview")
		if got, want := querycontract.IntVal(overview, "image_registry_match_count"), 0; got != want {
			t.Fatalf("deployment_overview.image_registry_match_count = %d, want %d (withheld ref must not count as a match)", got, want)
		}
		if _, ok := body["image_registry_truth"]; ok {
			t.Fatalf("image_registry_truth = %#v, want key absent (the only ref was withheld)", body["image_registry_truth"])
		}
	})
}

// httptestTraceDeploymentChain drives Handler.TraceDeploymentChain for
// service_name "orders-api" and returns the decoded 200 OK response body.
func httptestTraceDeploymentChain(t *testing.T, handler *Handler) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", strings.NewReader(`{"service_name":"orders-api"}`))
	recorder := httptest.NewRecorder()
	handler.TraceDeploymentChain(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}
