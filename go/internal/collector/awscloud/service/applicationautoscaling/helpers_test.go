// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package applicationautoscaling

import "testing"

// TestScalableTargetResourceID proves the scalable-target identity is the full
// (namespace, dimension, resource) triple. The scalable dimension is an
// identifying part of a registered target, so an empty dimension must yield ""
// rather than collapsing distinct read/write targets onto one id, and distinct
// dimensions for the same resource must produce distinct ids.
func TestScalableTargetResourceID(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		dimension string
		resource  string
		want      string
	}{
		{
			name:      "full triple",
			namespace: "dynamodb",
			dimension: "dynamodb:table:ReadCapacityUnits",
			resource:  "table/orders",
			want:      "dynamodb/dynamodb:table:ReadCapacityUnits/table/orders",
		},
		{
			name:      "trims whitespace on every part",
			namespace: "  ecs ",
			dimension: " ecs:service:DesiredCount ",
			resource:  " service/prod/api ",
			want:      "ecs/ecs:service:DesiredCount/service/prod/api",
		},
		{
			name:      "empty dimension is a missing identifying part",
			namespace: "dynamodb",
			dimension: "",
			resource:  "table/orders",
			want:      "",
		},
		{
			name:      "blank dimension is a missing identifying part",
			namespace: "dynamodb",
			dimension: "   ",
			resource:  "table/orders",
			want:      "",
		},
		{
			name:      "missing namespace",
			namespace: "",
			dimension: "dynamodb:table:ReadCapacityUnits",
			resource:  "table/orders",
			want:      "",
		},
		{
			name:      "missing resource",
			namespace: "dynamodb",
			dimension: "dynamodb:table:ReadCapacityUnits",
			resource:  "",
			want:      "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scalableTargetResourceID(tt.namespace, tt.dimension, tt.resource); got != tt.want {
				t.Fatalf("scalableTargetResourceID(%q,%q,%q) = %q, want %q",
					tt.namespace, tt.dimension, tt.resource, got, tt.want)
			}
		})
	}
}

// TestScalableTargetResourceIDDistinctDimensions proves two targets on the same
// resource but different dimensions never collapse onto the same id, which would
// merge a read-capacity target and a write-capacity target into one node.
func TestScalableTargetResourceIDDistinctDimensions(t *testing.T) {
	read := scalableTargetResourceID("dynamodb", "dynamodb:table:ReadCapacityUnits", "table/orders")
	write := scalableTargetResourceID("dynamodb", "dynamodb:table:WriteCapacityUnits", "table/orders")
	if read == "" || write == "" {
		t.Fatalf("expected non-empty ids, got read=%q write=%q", read, write)
	}
	if read == write {
		t.Fatalf("distinct dimensions collapsed onto one id: %q", read)
	}
}
