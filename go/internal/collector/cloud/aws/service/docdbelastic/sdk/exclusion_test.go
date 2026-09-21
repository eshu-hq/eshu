// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsDocumentsAndMutation is the metadata-only
// acceptance gate for DocumentDB Elastic Clusters: the SDK adapter must never
// read document contents, collections, indexes, or query results, must never
// read the admin password, and must never mutate Elastic Cluster state. We
// reflect over the adapter's read interface and confirm no document/query read,
// snapshot copy, or mutation method is reachable. This test fails the build if a
// future edit ever adds one of these to the adapter surface.
func TestAdapterInterfaceForbidsDocumentsAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// data-plane document / query reads — never reachable.
		"Document", "Collection", "Index", "Query", "Records", "Password", "Secret",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore", "Copy", "Apply", "Modify",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the DocumentDB Elastic read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden document/credential/mutation method %q; the DocumentDB Elastic adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the DocumentDB Elastic adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrGetReads asserts every method on the adapter
// interface is a List or Get control-plane read so the read surface stays
// explicit and auditable. The scanner lists clusters, gets each cluster's
// control-plane metadata, and reads resource tags only; nothing reads document
// payloads or snapshot contents.
func TestAdapterMethodsAreListOrGetReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is not a List or Get read", name)
		}
	}
}
