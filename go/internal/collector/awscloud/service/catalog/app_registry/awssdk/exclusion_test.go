// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsContentBodiesAndMutation is the metadata-only
// acceptance gate for AppRegistry: the SDK adapter must never read the
// attribute-group content body, never read an associated-resource tag value
// detail, and never write or mutate AppRegistry state. We reflect over the
// adapter's read interface and confirm no Get/Describe content-read, mutation,
// or association-write method is reachable. GetAttributeGroup,
// GetConfiguration, and GetAssociatedResource return content/tag detail and are
// excluded from the interface below. This test fails the build if a future edit
// ever adds one of these to the adapter surface.
func TestAdapterInterfaceForbidsContentBodiesAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// content-body / tag-value detail reads — never reachable.
		"Configuration", "GetAttributeGroup", "GetApplication", "GetAssociatedResource",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Sync", "Get", "Describe",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the AppRegistry read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden content/mutation method %q; the AppRegistry adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes content/mutation method %q (prefix %q); the AppRegistry adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListReads asserts every method on the adapter interface
// is a List read so the read surface stays explicit and auditable. The scanner
// reads application and attribute-group metadata, application associations, and
// resource tags only; nothing describes or fetches a content body.
func TestAdapterMethodsAreListReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is not a List read", name)
		}
	}
}
