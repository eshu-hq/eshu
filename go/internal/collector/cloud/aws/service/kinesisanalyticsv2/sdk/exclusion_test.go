// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsMutation is the metadata-only acceptance gate for
// the Managed Flink adapter: it must never create, update, delete, start, stop,
// add, or roll back an application, and must never read or write application
// code. We reflect over the adapter's read interface and confirm no mutation or
// code-write method is reachable. This test fails the build if a future edit
// ever adds one of these to the adapter surface.
func TestAdapterInterfaceForbidsMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// code / run-configuration writers are data-plane surfaces.
		"Code", "ApplicationCode",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Import", "Rollback",
		"Tag", "Untag", "Discover",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Managed Flink read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden method %q; the Managed Flink adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Managed Flink adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrDescribeReads asserts every method on the adapter
// interface is a List or Describe read so the read surface stays explicit and
// auditable. The scanner lists and describes application metadata, lists
// snapshots, and reads resource tags only; nothing fetches code or record
// payloads.
func TestAdapterMethodsAreListOrDescribeReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is not a List or Describe read", name)
		}
	}
}
