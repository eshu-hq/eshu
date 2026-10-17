// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"reflect"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func TestReopenBlockAttrsPreservesOrderTypesAndFreshness(t *testing.T) {
	first := ReopenBlockAttrs("deployment_mapping", 3, 2, 1)
	second := ReopenBlockAttrs("deployment_mapping", 3, 2, 1)
	if len(first) != 4 || len(second) != 4 {
		t.Fatalf("attribute lengths = %d, %d; want 4 each", len(first), len(second))
	}
	want := []attribute.KeyValue{
		attribute.String("domain", "deployment_mapping"),
		attribute.Int64("reopened", 3),
		attribute.Int64("skipped_by_memo", 2),
		attribute.Int64("skipped_unchanged", 1),
	}
	got := []attribute.KeyValue{first[0].spanAttribute(), first[1].spanAttribute(), first[2].spanAttribute(), first[3].spanAttribute()}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("span attributes = %v, want %v", got, want)
	}
	if fields := []string{first[0].logField(), first[1].logField(), first[2].logField(), first[3].logField()}; !reflect.DeepEqual(fields, []string{"domain=deployment_mapping", "reopened=3", "skipped_by_memo=2", "skipped_unchanged=1"}) {
		t.Fatalf("log fields = %v", fields)
	}
	first[0] = StringAttr("domain", "mutated")
	if got := second[0].spanAttribute().Value.AsString(); got != "deployment_mapping" {
		t.Fatalf("second call reused first slice: domain = %q", got)
	}
}
