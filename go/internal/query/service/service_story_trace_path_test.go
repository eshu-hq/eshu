// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// serviceTracePathTestDigest is the stable fixture digest for the trace-path
// image-package segment tests (mirrors the root supply-chain suite's
// serviceStoryTestDigest value).
const serviceTracePathTestDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestBuildServiceStoryResponseIncludesCodeToRuntimeTrace(t *testing.T) {
	t.Parallel()

	ctx := querytestutil.SampleServiceDossierContext()
	ctx["deployment_evidence"] = map[string]any{
		"artifacts": []map[string]any{
			{
				"relationship_type": "DEPLOYS_FROM",
				"resolved_id":       "resolved-gitops",
				"source_repo_name":  "deployment-charts",
				"path":              "argocd/prod/app.yaml",
				"artifact_family":   "argocd",
			},
		},
		"delivery_workflows": []map[string]any{
			{
				"tool_family": "github_actions",
				"path":        ".github/workflows/deploy.yml",
			},
		},
		"delivery_paths": []map[string]any{
			{
				"tool_family":       "kubernetes",
				"path":              "k8s/deployment.yaml",
				"container_images":  []string{"ghcr.io/acme/sample-service-api:1.2.3"},
				"runtime_platform":  "eks-prod",
				"relationship_type": "DEPLOYS_FROM",
			},
		},
		"shared_config_paths": []map[string]any{
			{
				"path":        "config/prod.yaml",
				"tool_family": "kustomize",
			},
		},
	}

	got := BuildServiceStoryResponse("sample-service-api", ctx)
	trace := querycontract.MapValue(got, "code_to_runtime_trace")
	if got, want := querycontract.StringVal(trace, "status"), "partial"; got != want {
		t.Fatalf("code_to_runtime_trace.status = %q, want %q", got, want)
	}
	segments := querycontract.MapSliceValue(trace, "segments")
	for _, want := range []string{
		"service_identity",
		"code_entrypoints",
		"ci_cd",
		"image_package",
		"deployment_config",
		"runtime",
		"cloud_dependencies",
	} {
		if querytestutil.SegmentByName(segments, want) == nil {
			t.Fatalf("code_to_runtime_trace.segments missing %q: %#v", want, segments)
		}
	}
	if got, want := querycontract.StringVal(querytestutil.SegmentByName(segments, "runtime"), "status"), "exact"; got != want {
		t.Fatalf("runtime segment status = %q, want %q", got, want)
	}
	imagePackage := querytestutil.SegmentByName(segments, "image_package")
	if got, want := querycontract.StringVal(imagePackage, "status"), "derived"; got != want {
		t.Fatalf("image_package segment status = %q, want %q", got, want)
	}
	evidence := querycontract.MapSliceValue(imagePackage, "evidence")
	if len(evidence) != 1 {
		t.Fatalf("image_package evidence = %#v, want one image/package row", evidence)
	}
	if got, want := querycontract.StringVal(evidence[0], "image_ref"), "ghcr.io/acme/sample-service-api:1.2.3"; got != want {
		t.Fatalf("image_package evidence image_ref = %q, want %q", got, want)
	}
	cloud := querytestutil.SegmentByName(segments, "cloud_dependencies")
	if got, want := querycontract.StringVal(cloud, "status"), "missing_evidence"; got != want {
		t.Fatalf("cloud_dependencies segment status = %q, want %q", got, want)
	}
	if got, want := querycontract.StringVal(cloud, "basis"), "cloud_resource_evidence"; got != want {
		t.Fatalf("cloud_dependencies segment basis = %q, want %q", got, want)
	}
	missing := querycontract.StringSliceVal(trace, "missing_segments")
	if !slices.Contains(missing, "cloud_dependencies") {
		t.Fatalf("missing_segments = %#v, want cloud_dependencies", missing)
	}
	encoded, err := json.Marshal(cloud)
	if err != nil {
		t.Fatalf("json.Marshal(cloud) error = %v, want nil", err)
	}
	if strings.Contains(string(encoded), `"evidence":null`) {
		t.Fatalf("cloud_dependencies evidence encoded as null, want empty array: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"evidence":[]`) {
		t.Fatalf("cloud_dependencies evidence = %s, want evidence empty array", encoded)
	}
}

func TestBuildServiceStoryTraceExplainsUncorrelatedCloudCandidates(t *testing.T) {
	t.Parallel()

	ctx := querytestutil.SampleServiceDossierContext()
	ctx["uncorrelated_cloud_resources"] = []map[string]any{
		{
			"id":                   "cloud:ssm:sample-service-client-port",
			"name":                 "/configd/sample-service/client/port",
			"resource_type":        "ssm_parameter",
			"provider":             "aws",
			"arn":                  "arn:aws:ssm:us-east-1:123456789012:parameter/configd/sample-service/client/port",
			"missing_relationship": "workload_cloud_relationship",
			"candidate_status":     "uncorrelated",
		},
	}

	got := BuildServiceStoryResponse("sample-service-api", ctx)
	trace := querycontract.MapValue(got, "code_to_runtime_trace")
	cloud := querytestutil.SegmentByName(querycontract.MapSliceValue(trace, "segments"), "cloud_dependencies")
	if cloud == nil {
		t.Fatalf("cloud_dependencies segment missing from trace: %#v", trace)
	}
	if got, want := querycontract.StringVal(cloud, "status"), "missing_evidence"; got != want {
		t.Fatalf("cloud_dependencies status = %q, want %q", got, want)
	}
	if got, want := querycontract.StringVal(cloud, "basis"), "uncorrelated_cloud_resource_candidates"; got != want {
		t.Fatalf("cloud_dependencies basis = %q, want %q", got, want)
	}
	if got, want := querycontract.IntVal(cloud, "candidate_count"), 1; got != want {
		t.Fatalf("cloud_dependencies candidate_count = %d, want %d", got, want)
	}
	if got, want := querycontract.StringVal(cloud, "missing_relationship"), "workload_cloud_relationship"; got != want {
		t.Fatalf("cloud_dependencies missing_relationship = %q, want %q", got, want)
	}
	evidence := querycontract.MapSliceValue(cloud, "evidence")
	if got, want := len(evidence), 1; got != want {
		t.Fatalf("cloud_dependencies evidence count = %d, want %d", got, want)
	}
	if got, want := querycontract.StringVal(evidence[0], "candidate_status"), "uncorrelated"; got != want {
		t.Fatalf("cloud candidate status = %q, want %q", got, want)
	}
	if resources := querycontract.MapSliceValue(ctx, "cloud_resources"); len(resources) != 0 {
		t.Fatalf("cloud_resources = %#v, want no promoted attached resources", resources)
	}
}

func TestServiceTraceImagePackageSegmentPreservesEvidenceWithPartialMissingReasons(t *testing.T) {
	t.Parallel()

	ctx := map[string]any{
		"supply_chain_evidence": map[string]any{
			"image_package": map[string]any{
				"evidence": []map[string]any{
					{
						"image_ref":              "registry.example.com/team/api:prod",
						"digest":                 serviceTracePathTestDigest,
						"sbom_attachment_id":     "sbom-attachment-1",
						"sbom_attachment_status": "attached_verified",
					},
				},
				"missing_evidence": []string{"container_image_identity_missing"},
			},
		},
	}

	segment := serviceTraceImagePackageSegment(ctx)
	if got, want := querycontract.StringVal(segment, "status"), "exact"; got != want {
		t.Fatalf("image_package status = %q, want %q; segment=%#v", got, want, segment)
	}
	evidence := querycontract.MapSliceValue(segment, "evidence")
	if got, want := len(evidence), 1; got != want {
		t.Fatalf("image_package evidence count = %d, want %d; segment=%#v", got, want, segment)
	}
	if got, want := querycontract.IntVal(segment, "evidence_count"), 1; got != want {
		t.Fatalf("image_package evidence_count = %d, want %d", got, want)
	}
	missing := querycontract.StringSliceVal(segment, "missing_evidence")
	if !slices.Contains(missing, "container_image_identity_missing") {
		t.Fatalf("missing_evidence = %#v, want preserved container_image_identity_missing", missing)
	}
}

func BenchmarkBuildServiceCodeToRuntimeTraceLargeDossier(b *testing.B) {
	ctx := querytestutil.SampleServiceDossierContext()
	ctx["api_surface"] = map[string]any{
		"endpoints": serviceTraceBenchmarkRows(250),
	}
	ctx["entrypoints"] = serviceTraceBenchmarkRows(100)
	ctx["deployment_evidence"] = map[string]any{
		"artifacts":               serviceTraceBenchmarkRows(250),
		"delivery_workflows":      serviceTraceBenchmarkRows(100),
		"delivery_paths":          serviceTraceBenchmarkRows(250),
		"shared_config_paths":     serviceTraceBenchmarkRows(250),
		"artifact_count":          250,
		"delivery_path_count":     250,
		"delivery_workflow_count": 100,
	}
	ctx["instances"] = serviceTraceBenchmarkRows(250)
	ctx["cloud_resources"] = serviceTraceBenchmarkRows(250)

	b.ReportAllocs()
	for b.Loop() {
		got := buildServiceCodeToRuntimeTrace(ctx)
		if querycontract.StringVal(got, "status") != "complete" {
			b.Fatalf("status = %#v, want complete", got["status"])
		}
	}
}

func serviceTraceBenchmarkRows(count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for range count {
		rows = append(rows, map[string]any{
			"path":             "services/checkout/deploy.yaml",
			"tool_family":      "kubernetes",
			"image_ref":        "ghcr.io/acme/checkout-api:1.2.3",
			"container_images": []string{"ghcr.io/acme/checkout-api:1.2.3"},
			"name":             "checkout-api",
			"environment":      "prod",
			"platform_name":    "eks-prod",
			"methods":          []string{"GET", "POST"},
			"operation_ids":    []string{"getCheckout", "createCheckout"},
		})
	}
	return rows
}
