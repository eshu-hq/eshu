// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsMutationAndResultReads is the metadata-only
// acceptance gate for Resilience Hub: the SDK adapter must never mutate
// Resilience Hub state, import resources, start an assessment, or read an
// assessment result, drift detail, recommendation body, or resolution payload.
// We reflect over the adapter's read interface and confirm no such method is
// reachable. This test fails the build if a future edit ever adds one to the
// adapter surface.
func TestAdapterInterfaceForbidsMutationAndResultReads(t *testing.T) {
	forbiddenSubstrings := []string{
		// assessment result / drift / recommendation reads — never reachable.
		"Recommendation", "ComplianceDrift", "ResourceDrift", "Compliance",
		"Resolution", "ResourceMapping", "Metric", "Alarm", "Sop", "Test",
		"Unsupported", "Template",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Start", "Stop", "Remove",
		"Add", "Import", "Publish", "Resolve", "Accept", "Reject", "Batch",
		"Tag", "Untag", "Associate", "Disassociate",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Resilience Hub read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden result/recommendation method %q; the Resilience Hub adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Resilience Hub adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrDescribeReads asserts every method on the adapter
// interface is a List or Describe read so the read surface stays explicit and
// auditable.
func TestAdapterMethodsAreListOrDescribeReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is not a List or Describe read", name)
		}
	}
}
