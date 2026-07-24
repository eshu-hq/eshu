// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"
)

// TestCloudInventoryResourceViewSurfacesZipLambdaCodeCorrelationGap proves that
// a zip-packaged Lambda function (package_type=Zip carrying code_sha256)
// explicitly surfaces the bounded code_sha256_correlation limitation on the
// cloud-inventory readback (issue #5454). The Lambda code_sha256 is
// base64(SHA256(the exact deployment .zip)); no CI, package-registry, or OCI
// hash Eshu collects covers those bytes, so the readback must state the gap
// programmatically rather than leave it silent.
func TestCloudInventoryResourceViewSurfacesZipLambdaCodeCorrelationGap(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"generation_id": "gen-1",
		"scope_id":      "aws:123456789012:us-east-1:lambda",
		"payload": map[string]any{
			"cloud_resource_uid":    "cloud_resource:aws-lambda-zip",
			"provider":              "aws",
			"resource_type":         "lambda.function",
			"management_origin":     "observed",
			"has_observed_evidence": true,
			"attributes": map[string]any{
				"package_type": "Zip",
				"code_sha256":  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
				"version":      "$LATEST",
			},
		},
	}

	view := cloudInventoryResourceView(envelope)
	label, ok := view[cloudInventoryCodeCorrelationKey].(map[string]any)
	if !ok {
		t.Fatalf("%s type = %T, want map[string]any (zip lambda must surface the bounded gap)",
			cloudInventoryCodeCorrelationKey, view[cloudInventoryCodeCorrelationKey])
	}
	if got := label["unsupported_reason"]; got != cloudInventoryZipCodeSHA256UnsupportedReason {
		t.Fatalf("unsupported_reason = %#v, want %q", got, cloudInventoryZipCodeSHA256UnsupportedReason)
	}
	if got := label["status"]; got != cloudInventoryCodeCorrelationStatusUncorrelated {
		t.Fatalf("status = %#v, want %q", got, cloudInventoryCodeCorrelationStatusUncorrelated)
	}
	if got := label["truth_basis"]; got != cloudInventoryCodeCorrelationTruthBasisDisplayOnly {
		t.Fatalf("truth_basis = %#v, want %q", got, cloudInventoryCodeCorrelationTruthBasisDisplayOnly)
	}
}

// TestCloudInventoryResourceViewOmitsCodeCorrelationGapForImageLambda proves an
// image-packaged Lambda (package_type=Image, image_uri present) does NOT carry
// the bounded code-correlation limitation: its deployment code is the container
// image, which #5450 correlates to the OCI ContainerImage via image_uri /
// resolved_image_uri. Surfacing the gap on an image Lambda would be a false
// negative.
func TestCloudInventoryResourceViewOmitsCodeCorrelationGapForImageLambda(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"generation_id": "gen-1",
		"scope_id":      "aws:123456789012:us-east-1:lambda",
		"payload": map[string]any{
			"cloud_resource_uid":    "cloud_resource:aws-lambda-image",
			"provider":              "aws",
			"resource_type":         "lambda.function",
			"management_origin":     "observed",
			"has_observed_evidence": true,
			"attributes": map[string]any{
				"package_type":       "Image",
				"image_uri":          "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
				"resolved_image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:0000000000000000000000000000000000000000000000000000000000cc",
				"code_sha256":        "0000000000000000000000000000000000000000000000000000000000cc",
				"version":            "$LATEST",
			},
		},
	}

	view := cloudInventoryResourceView(envelope)
	if _, present := view[cloudInventoryCodeCorrelationKey]; present {
		t.Fatalf("%s present for an image-packaged Lambda (its image_uri correlates via #5450): %#v",
			cloudInventoryCodeCorrelationKey, view[cloudInventoryCodeCorrelationKey])
	}
}

// TestCloudInventoryResourceViewOmitsCodeCorrelationGapForNonLambda proves a
// non-Lambda AWS resource (no package_type attribute) never carries the
// code-correlation limitation, even when it happens to surface other
// allowlisted attributes. The gap is Lambda-zip-specific.
func TestCloudInventoryResourceViewOmitsCodeCorrelationGapForNonLambda(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"generation_id": "gen-1",
		"scope_id":      "aws:123456789012:us-east-1:ecs",
		"payload": map[string]any{
			"cloud_resource_uid":    "cloud_resource:aws-ecs-task",
			"provider":              "aws",
			"resource_type":         "ecs.task",
			"management_origin":     "observed",
			"has_observed_evidence": true,
			"attributes": map[string]any{
				"task_definition_arn": "arn:aws:ecs:us-east-1:123456789012:task-definition/demo:1",
			},
		},
	}

	view := cloudInventoryResourceView(envelope)
	if _, present := view[cloudInventoryCodeCorrelationKey]; present {
		t.Fatalf("%s present for a non-Lambda resource: %#v",
			cloudInventoryCodeCorrelationKey, view[cloudInventoryCodeCorrelationKey])
	}
}

// TestCloudInventoryResourceViewOmitsCodeCorrelationGapForNonLambdaWithCollidingAttrs
// is the #5454 F1 regression: a NON-Lambda AWS resource (e.g. an OpenSearch
// package, which also emits a package_type attribute) that coincidentally
// carries package_type=Zip + a code_sha256-named key must NOT receive the
// zip-Lambda code-correlation gap label. The label gates on a closed Lambda
// resource_type set, not on the attribute names alone.
func TestCloudInventoryResourceViewOmitsCodeCorrelationGapForNonLambdaWithCollidingAttrs(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"generation_id": "gen-1",
		"scope_id":      "aws:123456789012:us-east-1:opensearch",
		"payload": map[string]any{
			"cloud_resource_uid":    "cloud_resource:aws-opensearch-package",
			"provider":              "aws",
			"resource_type":         "aws_opensearch_package",
			"management_origin":     "observed",
			"has_observed_evidence": true,
			"attributes": map[string]any{
				"package_type": "Zip",
				"code_sha256":  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			},
		},
	}

	view := cloudInventoryResourceView(envelope)
	if _, present := view[cloudInventoryCodeCorrelationKey]; present {
		t.Fatalf("%s present for a non-Lambda AWS resource with colliding attrs: %#v",
			cloudInventoryCodeCorrelationKey, view[cloudInventoryCodeCorrelationKey])
	}
}

// TestCloudInventoryResourceViewOmitsCodeCorrelationGapForNonAWSProvider is the
// #5454 F1 regression for the provider gate: a NON-AWS resource (GCP) reaching
// this provider-agnostic read model with package_type=Zip + code_sha256-named
// keys must NOT receive the AWS-Lambda-specific gap label.
func TestCloudInventoryResourceViewOmitsCodeCorrelationGapForNonAWSProvider(t *testing.T) {
	t.Parallel()

	envelope := map[string]any{
		"generation_id": "gen-1",
		"scope_id":      "gcp:demo-project",
		"payload": map[string]any{
			"cloud_resource_uid":    "cloud_resource:gcp-artifact",
			"provider":              "gcp",
			"resource_type":         "lambda.function", // even a colliding resource_type must not fire for gcp
			"management_origin":     "observed",
			"has_observed_evidence": true,
			"attributes": map[string]any{
				"package_type": "Zip",
				"code_sha256":  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			},
		},
	}

	view := cloudInventoryResourceView(envelope)
	if _, present := view[cloudInventoryCodeCorrelationKey]; present {
		t.Fatalf("%s present for a non-AWS provider: %#v",
			cloudInventoryCodeCorrelationKey, view[cloudInventoryCodeCorrelationKey])
	}
}
