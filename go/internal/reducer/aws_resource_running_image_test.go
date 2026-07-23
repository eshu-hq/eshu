// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

func TestExtractCloudResourceNodeRowsSurfacesECSRunningTaskImage(t *testing.T) {
	t.Parallel()

	for _, resourceType := range []string{"ecs.task", "aws_ecs_task"} {
		t.Run(resourceType, func(t *testing.T) {
			t.Parallel()
			rows, _, err := ExtractCloudResourceNodeRows([]facts.Envelope{
				awsResourceEnvelope(map[string]any{
					"account_id":    "123456789012",
					"region":        "us-east-1",
					"resource_type": resourceType,
					"resource_id":   "arn:aws:ecs:us-east-1:123456789012:task/demo/a",
					"attributes": map[string]any{
						"containers": []any{
							map[string]any{
								"image":        "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
								"image_digest": "sha256:aa",
								"name":         "demo",
							},
						},
					},
				}),
			})
			if err != nil {
				t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
			}
			if len(rows) != 1 {
				t.Fatalf("len(rows) = %d, want 1", len(rows))
			}
			row := rows[0]
			if got, want := anyToString(row["running_image_ref"]), "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest"; got != want {
				t.Fatalf("running_image_ref = %q, want %q", got, want)
			}
			if got, want := anyToString(row["running_image_digest"]), "sha256:aa"; got != want {
				t.Fatalf("running_image_digest = %q, want %q", got, want)
			}
		})
	}
}

func TestExtractCloudResourceNodeRowsSurfacesLambdaFunctionImage(t *testing.T) {
	t.Parallel()

	for _, resourceType := range []string{"lambda.function", "aws_lambda_function"} {
		t.Run(resourceType, func(t *testing.T) {
			t.Parallel()
			rows, _, err := ExtractCloudResourceNodeRows([]facts.Envelope{
				awsResourceEnvelope(map[string]any{
					"account_id":    "123456789012",
					"region":        "us-east-1",
					"resource_type": resourceType,
					"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:demo",
					"attributes": map[string]any{
						"package_type":       "Image",
						"image_uri":          "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
						"resolved_image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc",
					},
				}),
			})
			if err != nil {
				t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
			}
			if len(rows) != 1 {
				t.Fatalf("len(rows) = %d, want 1", len(rows))
			}
			row := rows[0]
			if got, want := anyToString(row["running_image_ref"]), "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest"; got != want {
				t.Fatalf("running_image_ref = %q, want %q", got, want)
			}
			// running_image_digest is normalized to the BARE digest (issue
			// #5450 P2), matching the shape ECS's TaskContainer.ImageDigest
			// already carries, not the full registry/repository@digest
			// reference resolved_image_uri itself carries (that full form
			// remains available via running_image_ref).
			if got, want := anyToString(row["running_image_digest"]), "sha256:cc"; got != want {
				t.Fatalf("running_image_digest = %q, want %q", got, want)
			}
		})
	}
}

// TestExtractCloudResourceNodeRowsSetsExplicitEmptyRunningImageForAmbiguousECSContainers
// proves a multi-container ("sidecar") ECS task — which has no single "the"
// running image — gets running_image_ref/running_image_digest as PRESENT
// keys with "" rather than omitted. Omitting the key was the pre-fix
// behavior and collided with the pinned NornicDB backend's
// missing-map-key-in-UNWIND bug (issue #5450, following the #4995
// precedent — see runningImageFieldsAbsent's doc in
// aws_resource_running_image.go): a heterogeneous row map in
// canonicalCloudResourceUpsertCypher's UNWIND $rows batch persisted the
// literal string "row.running_image_ref" instead of null for every row
// missing the key, live-proved against the pinned NornicDB image.
func TestExtractCloudResourceNodeRowsSetsExplicitEmptyRunningImageForAmbiguousECSContainers(t *testing.T) {
	t.Parallel()

	rows, _, err := ExtractCloudResourceNodeRows([]facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "123456789012",
			"region":        "us-east-1",
			"resource_type": "ecs.task",
			"resource_id":   "arn:aws:ecs:us-east-1:123456789012:task/demo/a",
			"attributes": map[string]any{
				"containers": []any{
					map[string]any{"image": "demo:latest", "image_digest": "sha256:aa", "name": "app"},
					map[string]any{"image": "sidecar:latest", "image_digest": "sha256:bb", "name": "sidecar"},
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if got, ok := row["running_image_ref"]; !ok || got != "" {
		t.Fatalf("running_image_ref = %v (present=%v), want present and \"\" for a multi-container ambiguous task", got, ok)
	}
	if got, ok := row["running_image_digest"]; !ok || got != "" {
		t.Fatalf("running_image_digest = %v (present=%v), want present and \"\" for a multi-container ambiguous task", got, ok)
	}
}

// TestExtractCloudResourceNodeRowsSetsExplicitEmptyRunningImageForNonImageResourceTypes
// proves a non-gated resource_type (not an ECS running task or Lambda
// function) gets running_image_ref/running_image_digest as PRESENT keys with
// "" rather than omitted, for the same UNWIND-batch reason documented on the
// ambiguous-ECS-containers test above.
func TestExtractCloudResourceNodeRowsSetsExplicitEmptyRunningImageForNonImageResourceTypes(t *testing.T) {
	t.Parallel()

	rows, _, err := ExtractCloudResourceNodeRows([]facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "123456789012",
			"region":        "us-east-1",
			"resource_type": "ecs.service",
			"resource_id":   "arn:aws:ecs:us-east-1:123456789012:service/demo/demo",
			"attributes": map[string]any{
				"cluster_arn": "arn:aws:ecs:us-east-1:123456789012:cluster/demo",
			},
		}),
	})
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if got, ok := row["running_image_ref"]; !ok || got != "" {
		t.Fatalf("running_image_ref = %v (present=%v), want present and \"\" for a non-image resource_type", got, ok)
	}
	if got, ok := row["running_image_digest"]; !ok || got != "" {
		t.Fatalf("running_image_digest = %v (present=%v), want present and \"\" for a non-image resource_type", got, ok)
	}
}

func TestExtractCloudResourceNodeRowsQuarantinesMalformedRunningImageContainer(t *testing.T) {
	t.Parallel()

	_, quarantined, err := ExtractCloudResourceNodeRows([]facts.Envelope{
		awsResourceEnvelope(map[string]any{
			"account_id":    "123456789012",
			"region":        "us-east-1",
			"resource_type": "ecs.task",
			"resource_id":   "arn:aws:ecs:us-east-1:123456789012:task/demo/a",
			"attributes": map[string]any{
				"containers": []any{
					map[string]any{"image": 5},
				},
			},
		}),
	})
	if err != nil {
		t.Fatalf("ExtractCloudResourceNodeRows() error = %v, want nil", err)
	}
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1", len(quarantined))
	}
}

// TestLambdaFunctionImageFieldsDigestShapeMatchesECS proves
// running_image_digest carries the SAME bare-digest shape for both ECS and
// Lambda (issue #5450 P2): before this fix, ECS emitted a bare digest
// ("sha256:...") while Lambda emitted the full registry/repository@digest
// reference, an undocumented divergence under one property name.
func TestLambdaFunctionImageFieldsDigestShapeMatchesECS(t *testing.T) {
	t.Parallel()

	t.Run("resolved_image_uri present: digest is extracted bare", func(t *testing.T) {
		resource := decodeTestAWSResourceForRunningImage(t, map[string]any{
			"account_id":    "123456789012",
			"region":        "us-east-1",
			"resource_type": "lambda.function",
			"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:demo",
			"attributes": map[string]any{
				"image_uri":          "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
				"resolved_image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc",
			},
		})
		fields, err := lambdaFunctionImageFields(resource)
		if err != nil {
			t.Fatalf("lambdaFunctionImageFields() error = %v", err)
		}
		if got, want := fields["running_image_ref"], "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest"; got != want {
			t.Fatalf("running_image_ref = %v, want %v", got, want)
		}
		if got, want := fields["running_image_digest"], "sha256:cc"; got != want {
			t.Fatalf("running_image_digest = %v, want %v (bare, matching ECS's shape)", got, want)
		}
	})

	t.Run("resolved_image_uri absent: no running_image_digest, never a fabricated value", func(t *testing.T) {
		resource := decodeTestAWSResourceForRunningImage(t, map[string]any{
			"account_id":    "123456789012",
			"region":        "us-east-1",
			"resource_type": "lambda.function",
			"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:demo",
			"attributes": map[string]any{
				"image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
			},
		})
		fields, err := lambdaFunctionImageFields(resource)
		if err != nil {
			t.Fatalf("lambdaFunctionImageFields() error = %v", err)
		}
		if got, ok := fields["running_image_digest"]; !ok || got != "" {
			t.Fatalf("running_image_digest = %v (present=%v), want present and \"\" when resolved_image_uri is absent", got, ok)
		}
	})

	t.Run("resolved_image_uri present but tag-only (unexpected shape): no fabricated digest", func(t *testing.T) {
		resource := decodeTestAWSResourceForRunningImage(t, map[string]any{
			"account_id":    "123456789012",
			"region":        "us-east-1",
			"resource_type": "lambda.function",
			"resource_id":   "arn:aws:lambda:us-east-1:123456789012:function:demo",
			"attributes": map[string]any{
				"image_uri":          "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
				"resolved_image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
			},
		})
		fields, err := lambdaFunctionImageFields(resource)
		if err != nil {
			t.Fatalf("lambdaFunctionImageFields() error = %v", err)
		}
		if got, ok := fields["running_image_digest"]; !ok || got != "" {
			t.Fatalf("running_image_digest = %v (present=%v), want present and \"\" for a non-digest-qualified resolved_image_uri", got, ok)
		}
	})
}

// decodeTestAWSResourceForRunningImage decodes a synthetic aws_resource
// payload through the same factschema seam production code uses, so this
// file's unit tests exercise the real decode path (not a hand-built struct
// literal that could silently drift from the typed contract).
func decodeTestAWSResourceForRunningImage(t *testing.T, payload map[string]any) awsv1.Resource {
	t.Helper()
	resource, err := decodeAWSResource(awsResourceEnvelope(payload))
	if err != nil {
		t.Fatalf("decodeAWSResource() error = %v", err)
	}
	return resource
}
