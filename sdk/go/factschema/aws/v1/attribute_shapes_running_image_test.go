// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import "testing"

func TestDecodeResourceECSTaskAttributes(t *testing.T) {
	t.Run("valid containers array (JSON round-trip shape) decodes", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{
				"attributes": map[string]any{
					"containers": []any{
						map[string]any{
							"image":        "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
							"image_digest": "sha256:aa",
							"name":         "demo",
							"runtime_id":   "bb",
						},
					},
				},
			},
		}
		got, err := DecodeResourceECSTaskAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.Containers) != 1 {
			t.Fatalf("len(Containers) = %d, want 1", len(got.Containers))
		}
		if got.Containers[0].Image != "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest" {
			t.Fatalf("Containers[0].Image = %q", got.Containers[0].Image)
		}
		if got.Containers[0].ImageDigest != "sha256:aa" {
			t.Fatalf("Containers[0].ImageDigest = %q", got.Containers[0].ImageDigest)
		}
	})

	t.Run("valid containers array (in-Go []map[string]string shape) decodes identically", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{
				"attributes": map[string]any{
					"containers": []map[string]string{
						{"image": "demo:latest", "image_digest": "sha256:aa", "name": "demo", "runtime_id": "bb"},
					},
				},
			},
		}
		got, err := DecodeResourceECSTaskAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got.Containers) != 1 || got.Containers[0].Image != "demo:latest" || got.Containers[0].ImageDigest != "sha256:aa" {
			t.Fatalf("Containers = %+v", got.Containers)
		}
	})

	t.Run("absent containers decodes as nil, not an error", func(t *testing.T) {
		resource := Resource{Attributes: map[string]any{"attributes": map[string]any{}}}
		got, err := DecodeResourceECSTaskAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Containers != nil {
			t.Fatalf("Containers = %v, want nil", got.Containers)
		}
	})

	t.Run("containers present as a non-array is a visible decode failure", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{"attributes": map[string]any{"containers": "not-a-list"}},
		}
		_, err := DecodeResourceECSTaskAttributes(resource)
		if err == nil {
			t.Fatal("want error for containers present as a non-array, got nil")
		}
	})

	t.Run("container image present as wrong type is a visible decode failure", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{
				"attributes": map[string]any{
					"containers": []any{map[string]any{"image": 5}},
				},
			},
		}
		_, err := DecodeResourceECSTaskAttributes(resource)
		if err == nil {
			t.Fatal("want error for container image present as a non-string, got nil")
		}
	})
}

func TestDecodeResourceLambdaFunctionImageAttributes(t *testing.T) {
	t.Run("valid image_uri and resolved_image_uri decode", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{
				"attributes": map[string]any{
					"image_uri":          "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest",
					"resolved_image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc",
				},
			},
		}
		got, err := DecodeResourceLambdaFunctionImageAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ImageURI != "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo:latest" {
			t.Fatalf("ImageURI = %q", got.ImageURI)
		}
		if got.ResolvedImageURI != "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc" {
			t.Fatalf("ResolvedImageURI = %q", got.ResolvedImageURI)
		}
	})

	t.Run("absent fields decode as empty string, not an error", func(t *testing.T) {
		resource := Resource{Attributes: map[string]any{"attributes": map[string]any{}}}
		got, err := DecodeResourceLambdaFunctionImageAttributes(resource)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ImageURI != "" || got.ResolvedImageURI != "" {
			t.Fatalf("got = %+v, want empty", got)
		}
	})

	t.Run("resolved_image_uri present as wrong type is a visible decode failure", func(t *testing.T) {
		resource := Resource{
			Attributes: map[string]any{"attributes": map[string]any{"resolved_image_uri": 5}},
		}
		_, err := DecodeResourceLambdaFunctionImageAttributes(resource)
		if err == nil {
			t.Fatal("want error for resolved_image_uri present as a non-string, got nil")
		}
	})
}

func TestDecodeRelationshipLambdaFunctionUsesImageAttributes(t *testing.T) {
	t.Run("valid package_type and resolved_image_uri decode", func(t *testing.T) {
		rel := Relationship{
			Attributes: map[string]any{
				"attributes": map[string]any{
					"package_type":       "Image",
					"resolved_image_uri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc",
				},
			},
		}
		got, err := DecodeRelationshipLambdaFunctionUsesImageAttributes(rel)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.PackageType != "Image" {
			t.Fatalf("PackageType = %q", got.PackageType)
		}
		if got.ResolvedImageURI != "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo@sha256:cc" {
			t.Fatalf("ResolvedImageURI = %q", got.ResolvedImageURI)
		}
	})

	t.Run("absent fields decode as empty string, not an error", func(t *testing.T) {
		rel := Relationship{Attributes: map[string]any{"attributes": map[string]any{}}}
		got, err := DecodeRelationshipLambdaFunctionUsesImageAttributes(rel)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.PackageType != "" || got.ResolvedImageURI != "" {
			t.Fatalf("got = %+v, want empty", got)
		}
	})

	t.Run("resolved_image_uri present as wrong type is a visible decode failure", func(t *testing.T) {
		rel := Relationship{
			Attributes: map[string]any{"attributes": map[string]any{"resolved_image_uri": 5}},
		}
		_, err := DecodeRelationshipLambdaFunctionUsesImageAttributes(rel)
		if err == nil {
			t.Fatal("want error for resolved_image_uri present as a non-string, got nil")
		}
	})
}
