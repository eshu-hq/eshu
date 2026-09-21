// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfacesForbidFindingsAndMutation is the metadata-only acceptance
// gate for CodeGuru: neither the Reviewer nor the Profiler adapter interface may
// read code-review findings, recommendation content, profiling samples, flame
// graphs, or agent telemetry, and neither may mutate CodeGuru state. We reflect
// over both read interfaces and confirm no findings/profiling/code-review read,
// no mutation method, and nothing that is not a List/Describe read is reachable.
// This test fails the build if a future edit ever adds one of these to either
// adapter surface.
func TestAdapterInterfacesForbidFindingsAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// Reviewer findings / recommendation / code-review reads.
		"CodeReview", "Recommendation", "Feedback",
		// Profiler profiling-data reads.
		"Profile", "FindingsReport", "FrameMetric", "FrameMetricData",
		// Generic data-plane reads.
		"Findings",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Post", "Configure",
		"Associate", "Disassociate", "Add", "Remove", "Register",
		"Deregister", "Tag", "Untag", "Batch", "Get", "Submit",
		"Start", "Stop", "Enable", "Disable",
	}

	cases := []struct {
		name  string
		iface reflect.Type
	}{
		{"reviewerAPIClient", reflect.TypeOf((*reviewerAPIClient)(nil)).Elem()},
		{"profilerAPIClient", reflect.TypeOf((*profilerAPIClient)(nil)).Elem()},
	}
	for _, tc := range cases {
		if tc.iface.NumMethod() == 0 {
			t.Fatalf("%s has no methods; expected the CodeGuru read surface", tc.name)
		}
		for i := 0; i < tc.iface.NumMethod(); i++ {
			name := tc.iface.Method(i).Name
			for _, banned := range forbiddenSubstrings {
				if strings.Contains(name, banned) {
					t.Fatalf("%s exposes forbidden findings/profiling method %q; the CodeGuru adapter is metadata-only", tc.name, name)
				}
			}
			for _, prefix := range forbiddenPrefixes {
				if strings.HasPrefix(name, prefix) {
					t.Fatalf("%s exposes mutation/data method %q (prefix %q); the CodeGuru adapter is metadata-only", tc.name, name, prefix)
				}
			}
		}
	}
}

// TestAdapterMethodsAreListOrDescribeReads asserts every method on both adapter
// interfaces is a List or Describe read so the read surface stays explicit and
// auditable. The scanner reads association and profiling-group metadata and
// resource tags only; nothing fetches findings, recommendations, or profiling
// sample payloads.
func TestAdapterMethodsAreListOrDescribeReads(t *testing.T) {
	ifaces := []reflect.Type{
		reflect.TypeOf((*reviewerAPIClient)(nil)).Elem(),
		reflect.TypeOf((*profilerAPIClient)(nil)).Elem(),
	}
	for _, iface := range ifaces {
		for i := 0; i < iface.NumMethod(); i++ {
			name := iface.Method(i).Name
			if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Describe") {
				t.Fatalf("adapter method %q is not a List or Describe read", name)
			}
		}
	}
}
