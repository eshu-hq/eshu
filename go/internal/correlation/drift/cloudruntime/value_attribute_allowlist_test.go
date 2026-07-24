// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudruntime

import (
	"reflect"
	"testing"
)

func TestValueAttributeAllowlistFor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		resourceType string
		want         []string
	}{
		{name: "aws_instance_ami", resourceType: "aws_instance", want: []string{"ami"}},
		{
			name:         "aws_lambda_function_image_and_version",
			resourceType: "aws_lambda_function",
			want:         []string{"image_uri", "version"},
		},
		{name: "unknown_resource_type_returns_nil", resourceType: "aws_s3_bucket", want: nil},
		{name: "blank_resource_type_returns_nil", resourceType: "", want: nil},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ValueAttributeAllowlistFor(tc.resourceType)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ValueAttributeAllowlistFor(%q) = %#v, want %#v", tc.resourceType, got, tc.want)
			}
		})
	}
}

// TestValueAttributeAllowlistForReturnsCopy proves the returned slice is a
// defensive copy: mutating it must never corrupt the package-level allowlist
// for subsequent callers.
func TestValueAttributeAllowlistForReturnsCopy(t *testing.T) {
	t.Parallel()

	got := ValueAttributeAllowlistFor("aws_instance")
	if len(got) == 0 {
		t.Fatalf("expected non-empty allowlist for aws_instance")
	}
	got[0] = "mutated"

	again := ValueAttributeAllowlistFor("aws_instance")
	if again[0] != "ami" {
		t.Fatalf("ValueAttributeAllowlistFor mutation leaked into package state: got %#v", again)
	}
}

func TestValueAttributeAllowlistResourceTypes(t *testing.T) {
	t.Parallel()

	got := ValueAttributeAllowlistResourceTypes()
	want := []string{"aws_instance", "aws_lambda_function"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ValueAttributeAllowlistResourceTypes() = %#v, want %#v", got, want)
	}
}
