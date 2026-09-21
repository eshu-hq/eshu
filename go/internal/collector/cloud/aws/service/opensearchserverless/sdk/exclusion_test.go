// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsDataPlaneAndMutation is the metadata-only acceptance
// gate for OpenSearch Serverless: the SDK adapter must never reach the OpenSearch
// HTTP data plane (index, search, bulk, document APIs) and must never mutate
// Serverless state. We reflect over the adapter's read interface and confirm no
// index/search/document, create/update/delete, or other mutation method is
// reachable. The control-plane data-plane APIs live on the collection endpoint
// this package never constructs, so they cannot appear at all; this test fails
// the build if a future edit ever adds a mutation method to the adapter surface.
func TestAdapterInterfaceForbidsDataPlaneAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// OpenSearch HTTP data plane — never reachable from the control plane.
		"Index", "Search", "Bulk", "Document", "Query",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Import", "Tag", "Untag",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the OpenSearch Serverless read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden data-plane method %q; the adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreReads asserts every method on the adapter interface is a
// List, BatchGet, or Get read so the read surface stays explicit and auditable.
func TestAdapterMethodsAreReads(t *testing.T) {
	allowedPrefixes := []string{"List", "BatchGet", "Get"}
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		ok := false
		for _, prefix := range allowedPrefixes {
			if strings.HasPrefix(name, prefix) {
				ok = true
				break
			}
		}
		if !ok {
			t.Fatalf("apiClient method %q is not a List/BatchGet/Get read", name)
		}
	}
}
