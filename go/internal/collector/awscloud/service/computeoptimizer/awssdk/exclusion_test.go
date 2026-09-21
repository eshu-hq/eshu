// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsMutation is the metadata-only acceptance gate for
// Compute Optimizer: the SDK adapter must never mutate Compute Optimizer state,
// never change enrollment, and never start an export. We reflect over the
// adapter's read interface and confirm no mutation or export method is reachable.
// This test fails the build if a future edit ever adds one to the adapter
// surface.
func TestAdapterInterfaceForbidsMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		"EnrollmentStatus", "Export", "Preferences",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Compute Optimizer read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden method %q (substring %q); the adapter is metadata-only", name, banned)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreGetReads asserts every method on the adapter interface is
// a Get read so the read surface stays explicit and auditable. The scanner reads
// recommendation summaries and per-resource recommendations only.
func TestAdapterMethodsAreGetReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is not a Get read", name)
		}
	}
}
