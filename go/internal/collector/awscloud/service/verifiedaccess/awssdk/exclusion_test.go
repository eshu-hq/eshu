// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsMutationAndPolicyReads is the metadata-only
// acceptance gate for Verified Access: the SDK adapter must never mutate
// Verified Access state and must never read a group/endpoint policy document or
// trust-provider secret. Although Verified Access ships under the EC2 SDK (which
// exposes Create/Modify/Delete and policy-read operations), the adapter
// interface below lists only the four Describe reads. We reflect over it and
// confirm no mutation or policy-read method is reachable. This test fails the
// build if a future edit ever adds one.
func TestAdapterInterfaceForbidsMutationAndPolicyReads(t *testing.T) {
	forbiddenSubstrings := []string{
		// policy-body / data-plane reads — never reachable.
		"Policy", "Logging", "Token",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Modify", "Put", "Write",
		"Stop", "Start", "Add", "Remove", "Disable", "Enable",
		"Register", "Deregister", "Associate", "Disassociate",
		"Send", "Batch", "Import", "Export", "Tag", "Untag",
		"Attach", "Detach", "Get", "Allocate", "Release",
		"Accept", "Reject", "Replace", "Reset", "Revoke",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Verified Access describe surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden policy/data method %q; the Verified Access adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation/read-secret method %q (prefix %q); the Verified Access adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreDescribeReads asserts every method on the adapter
// interface is a Verified Access Describe read so the read surface stays
// explicit and auditable.
func TestAdapterMethodsAreDescribeReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "DescribeVerifiedAccess") {
			t.Fatalf("apiClient method %q is not a Verified Access Describe read", name)
		}
	}
}
