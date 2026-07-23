// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
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
			if got, want := anyToString(row["running_image_digest"]), "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc"; got != want {
				t.Fatalf("running_image_digest = %q, want %q", got, want)
			}
		})
	}
}

func TestExtractCloudResourceNodeRowsOmitsRunningImageForAmbiguousECSContainers(t *testing.T) {
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
	if _, ok := row["running_image_ref"]; ok {
		t.Fatalf("running_image_ref = %v, want absent for a multi-container ambiguous task", row["running_image_ref"])
	}
	if _, ok := row["running_image_digest"]; ok {
		t.Fatalf("running_image_digest = %v, want absent for a multi-container ambiguous task", row["running_image_digest"])
	}
}

func TestExtractCloudResourceNodeRowsOmitsRunningImageForNonImageResourceTypes(t *testing.T) {
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
	if _, ok := row["running_image_ref"]; ok {
		t.Fatalf("running_image_ref = %v, want absent for a non-image resource_type", row["running_image_ref"])
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
