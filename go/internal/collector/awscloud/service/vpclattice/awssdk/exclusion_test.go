// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsPolicyAndMutation is the metadata-only acceptance
// gate for VPC Lattice: the SDK adapter must never read auth-policy or
// resource-policy bodies and must never mutate VPC Lattice state. We reflect
// over the adapter's read interface and confirm no policy-read, write, or
// mutation method is reachable. GetAuthPolicy and GetResourcePolicy are
// excluded from the interface below; this test fails the build if a future edit
// ever adds one of these to the adapter surface.
func TestAdapterInterfaceForbidsPolicyAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// policy bodies — never reachable.
		"AuthPolicy", "ResourcePolicy", "Policy",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore", "Set",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the VPC Lattice read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden policy/mutation method %q; the VPC Lattice adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the VPC Lattice adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrGetReads asserts every method on the adapter
// interface is a List or Get read so the read surface stays explicit and
// auditable. The scanner reads service network, service, target group, and
// listener metadata plus per-service and per-target-group detail and resource
// tags; nothing reads a policy body or any data-plane payload.
func TestAdapterMethodsAreListOrGetReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is not a List or Get read", name)
		}
	}
}
