// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsQueryAndMutation is the metadata-only acceptance
// gate for Clean Rooms: the SDK adapter must never run a protected query or job,
// never read query results or analysis-rule bodies, and never mutate Clean Rooms
// state. We reflect over the adapter's read interface and confirm no
// query/job/result/mutation method is reachable. This test fails the build if a
// future edit ever adds one to the adapter surface.
func TestAdapterInterfaceForbidsQueryAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// protected-query / job execution and result reads — never reachable.
		"ProtectedQuery", "ProtectedJob", "Query", "Job", "Result",
		// analysis-rule / analysis-template bodies are not metadata.
		"AnalysisRule", "AnalysisTemplate", "Schema",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore", "Preview", "Populate",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Clean Rooms read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden method %q (substring %q); the Clean Rooms adapter is metadata-only", name, banned)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Clean Rooms adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrGetReads asserts every method on the adapter
// interface is a List or Get read so the read surface stays explicit and
// auditable. The scanner reads collaboration/configured-table/membership
// metadata, the single configured-table detail needed to resolve the Glue
// backing-table reference, and resource tags only.
func TestAdapterMethodsAreListOrGetReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is not a List or Get read", name)
		}
	}
}
