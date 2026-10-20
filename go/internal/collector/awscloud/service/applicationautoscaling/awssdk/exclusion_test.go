// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsMutation is the metadata-only acceptance gate for
// Application Auto Scaling: the SDK adapter must never register, deregister,
// mutate, or invoke a scaling action. We reflect over the adapter's read
// interface and confirm no register/put/delete/mutation method is reachable.
// This test fails the build if a future edit ever adds one of these to the
// adapter surface.
func TestAdapterInterfaceForbidsMutation(t *testing.T) {
	forbiddenPrefixes := []string{
		"Register", "Deregister", "Put", "Delete", "Create", "Update",
		"Start", "Stop", "Add", "Remove", "Set", "Modify", "Execute",
		"Tag", "Untag", "Enable", "Disable", "Suspend", "Resume",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Application Auto Scaling read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreDescribeReads asserts every method on the adapter
// interface is a Describe read so the read surface stays explicit and
// auditable. The scanner reads scalable target, scaling policy, and scheduled
// action metadata only.
func TestAdapterMethodsAreDescribeReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is not a Describe read", name)
		}
	}
}
