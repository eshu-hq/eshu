// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsMutationAndSecrets is the metadata-only acceptance
// gate for QuickSight: the SDK adapter must never mutate QuickSight state and
// must never read data-source credentials, connection secrets, embed URLs, or
// permissions. We reflect over the adapter's read interface and confirm no
// mutation, credential, secret, permission, embed, or ingestion method is
// reachable. This test fails the build if a future edit adds one to the adapter
// surface.
func TestAdapterInterfaceForbidsMutationAndSecrets(t *testing.T) {
	forbiddenSubstrings := []string{
		// credential / secret / permission reads must never be reachable.
		"Credential", "Secret", "Permission", "Embed", "Password",
		// ingestion / job control is a data-plane / mutation surface.
		"Ingestion", "Refresh", "SnapshotJob",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Start", "Stop",
		"Cancel", "Generate", "Register", "Search", "Tag", "Untag",
		"Restore", "Import", "Batch",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the QuickSight read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden method %q; the QuickSight adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the QuickSight adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrDescribeReads asserts every method on the adapter
// interface is a List or Describe read so the read surface stays explicit and
// auditable. The scanner reads resource metadata and tags only; it never reads a
// credential, SQL body, or visual definition payload.
func TestAdapterMethodsAreListOrDescribeReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is not a List or Describe read", name)
		}
	}
}
