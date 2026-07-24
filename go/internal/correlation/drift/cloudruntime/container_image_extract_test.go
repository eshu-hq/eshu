// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudruntime

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestExtractDeclaredContainerImages(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                string
		containerDefinition any
		wantImages          []string
		wantTruncated       bool
	}{
		{
			name:                "single_container",
			containerDefinition: `[{"name":"app","image":"123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest"}]`,
			wantImages:          []string{"123456789012.dkr.ecr.us-east-1.amazonaws.com/supply-chain-demo:latest"},
		},
		{
			name:                "multiple_distinct_images",
			containerDefinition: `[{"name":"app","image":"repo/app:v1"},{"name":"sidecar","image":"repo/sidecar:v1"}]`,
			wantImages:          []string{"repo/app:v1", "repo/sidecar:v1"},
		},
		{
			name:                "duplicate_images_deduplicated",
			containerDefinition: `[{"name":"app","image":"repo/app:v1"},{"name":"app-replica","image":"repo/app:v1"}]`,
			wantImages:          []string{"repo/app:v1"},
		},
		{
			name:                "blank_image_skipped",
			containerDefinition: `[{"name":"app","image":""},{"name":"sidecar","image":"repo/sidecar:v1"}]`,
			wantImages:          []string{"repo/sidecar:v1"},
		},
		{
			name:                "not_a_string_yields_nothing",
			containerDefinition: []any{map[string]any{"image": "repo/app:v1"}},
			wantImages:          nil,
		},
		{
			name:                "malformed_json_yields_nothing",
			containerDefinition: `not json`,
			wantImages:          nil,
		},
		{
			name:                "nil_input_yields_nothing",
			containerDefinition: nil,
			wantImages:          nil,
		},
		{
			name:                "empty_array_yields_nothing",
			containerDefinition: `[]`,
			wantImages:          nil,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractDeclaredContainerImages(tc.containerDefinition)
			if !reflect.DeepEqual(got.Images, tc.wantImages) {
				t.Fatalf("ExtractDeclaredContainerImages(%v).Images = %#v, want %#v", tc.containerDefinition, got.Images, tc.wantImages)
			}
			if got.Truncated != tc.wantTruncated {
				t.Fatalf("ExtractDeclaredContainerImages(%v).Truncated = %v, want %v", tc.containerDefinition, got.Truncated, tc.wantTruncated)
			}
		})
	}
}

// TestExtractDeclaredContainerImagesCapsAtBound proves the extractor never
// returns more than MaxContainerImagesPerResource images and flags
// Truncated when the source container_definitions document exceeds the
// bound, regardless of how many containers the declaration lists.
func TestExtractDeclaredContainerImagesCapsAtBound(t *testing.T) {
	t.Parallel()

	var containers []string
	for i := 0; i < MaxContainerImagesPerResource+5; i++ {
		containers = append(containers, fmt.Sprintf(`{"name":"c%d","image":"repo/service:%d"}`, i, i))
	}
	raw := "[" + strings.Join(containers, ",") + "]"

	got := ExtractDeclaredContainerImages(raw)
	if len(got.Images) != MaxContainerImagesPerResource {
		t.Fatalf("ExtractDeclaredContainerImages() returned %d images, want capped at %d", len(got.Images), MaxContainerImagesPerResource)
	}
	if !got.Truncated {
		t.Fatalf("ExtractDeclaredContainerImages() Truncated = false, want true when source exceeds the bound")
	}
}

// TestExtractDeclaredContainerImagesNeverLeaksNonImageFields is the mandatory
// security proof: a container_definitions document carrying environment
// variables, secrets ARNs, log configuration, and command overrides must
// yield ONLY the bounded image strings. No other field may ever appear in
// the extractor's output, whether by value or by any encoding of it.
func TestExtractDeclaredContainerImagesNeverLeaksNonImageFields(t *testing.T) {
	t.Parallel()

	const secretMarker = "TOTALLY-SECRET-VALUE-DO-NOT-LEAK"
	raw := `[{
		"name": "app",
		"image": "repo/app:v1",
		"environment": [{"name": "DATABASE_PASSWORD", "value": "` + secretMarker + `"}],
		"secrets": [{"name": "API_KEY", "valueFrom": "arn:aws:secretsmanager:us-east-1:123456789012:secret:api-key-AbCdEf"}],
		"logConfiguration": {"logDriver": "awslogs", "options": {"awslogs-group": "/ecs/app"}},
		"command": ["--admin-token=` + secretMarker + `"],
		"entryPoint": ["/bin/sh", "-c"],
		"portMappings": [{"containerPort": 8080}]
	}]`

	got := ExtractDeclaredContainerImages(raw)

	if !reflect.DeepEqual(got.Images, []string{"repo/app:v1"}) {
		t.Fatalf("ExtractDeclaredContainerImages() = %#v, want only [repo/app:v1]", got.Images)
	}

	// Belt-and-suspenders: prove the secret marker cannot reach the output
	// through any field, by checking the full formatted representation of
	// the result value.
	rendered := fmt.Sprintf("%#v", got)
	if strings.Contains(rendered, secretMarker) {
		t.Fatalf("ExtractDeclaredContainerImages() leaked a non-image field into its result: %s", rendered)
	}
	if strings.Contains(rendered, "secretsmanager") {
		t.Fatalf("ExtractDeclaredContainerImages() leaked a secret ARN reference into its result: %s", rendered)
	}

	// The result type itself must carry no field capable of holding
	// anything but the bounded image list and the truncation flag.
	resultType := reflect.TypeOf(got)
	if resultType.NumField() != 2 {
		t.Fatalf("ContainerImageExtractionResult has %d fields, want exactly 2 (Images, Truncated) to keep the security bound reviewable: %#v", resultType.NumField(), got)
	}
}

func TestExtractObservedContainerImages(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		containers any
		wantImages []string
	}{
		{
			name: "single_running_container",
			containers: []any{
				map[string]any{"image": "repo/app:v1", "image_digest": "sha256:aa", "name": "app", "runtime_id": "abc"},
			},
			wantImages: []string{"repo/app:v1"},
		},
		{
			name: "task_definition_shape_with_secrets_and_environment",
			containers: []any{
				map[string]any{
					"image":       "repo/app:v1",
					"name":        "app",
					"essential":   true,
					"environment": []any{map[string]any{"name": "PASSWORD", "value": "shh"}},
					"secrets":     []any{map[string]any{"name": "API_KEY", "value_from": "arn:aws:secretsmanager:us-east-1:123456789012:secret:x"}},
				},
			},
			wantImages: []string{"repo/app:v1"},
		},
		{name: "nil_input", containers: nil, wantImages: nil},
		{name: "wrong_type", containers: "not-a-list", wantImages: nil},
		{name: "empty_list", containers: []any{}, wantImages: nil},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractObservedContainerImages(tc.containers)
			if !reflect.DeepEqual(got.Images, tc.wantImages) {
				t.Fatalf("ExtractObservedContainerImages(%v).Images = %#v, want %#v", tc.containers, got.Images, tc.wantImages)
			}
		})
	}
}

// TestExtractObservedContainerImagesNeverLeaksNonImageFields mirrors the
// declared-side security proof for the observed (AWS-reported) container
// shape, which the ECS collector already carries environment/secrets on
// (see go/internal/collector/awscloud/services/ecs/scanner.go
// containerMaps).
func TestExtractObservedContainerImagesNeverLeaksNonImageFields(t *testing.T) {
	t.Parallel()

	const secretMarker = "TOTALLY-SECRET-VALUE-DO-NOT-LEAK"
	containers := []any{
		map[string]any{
			"image":       "repo/app:v1",
			"name":        "app",
			"essential":   true,
			"environment": []any{map[string]any{"name": "DATABASE_PASSWORD", "value": secretMarker}},
			"secrets":     []any{map[string]any{"name": "API_KEY", "value_from": secretMarker}},
		},
	}

	got := ExtractObservedContainerImages(containers)
	if !reflect.DeepEqual(got.Images, []string{"repo/app:v1"}) {
		t.Fatalf("ExtractObservedContainerImages() = %#v, want only [repo/app:v1]", got.Images)
	}
	rendered := fmt.Sprintf("%#v", got)
	if strings.Contains(rendered, secretMarker) {
		t.Fatalf("ExtractObservedContainerImages() leaked a non-image field into its result: %s", rendered)
	}
}

// TestExtractObservedContainerImagesCapsAtBound mirrors the declared-side
// cap proof for the AWS-observed container shape.
func TestExtractObservedContainerImagesCapsAtBound(t *testing.T) {
	t.Parallel()

	containers := make([]any, 0, MaxContainerImagesPerResource+5)
	for i := 0; i < MaxContainerImagesPerResource+5; i++ {
		containers = append(containers, map[string]any{"image": fmt.Sprintf("repo/service:%d", i)})
	}

	got := ExtractObservedContainerImages(containers)
	if len(got.Images) != MaxContainerImagesPerResource {
		t.Fatalf("ExtractObservedContainerImages() returned %d images, want capped at %d", len(got.Images), MaxContainerImagesPerResource)
	}
	if !got.Truncated {
		t.Fatalf("ExtractObservedContainerImages() Truncated = false, want true when source exceeds the bound")
	}
}
