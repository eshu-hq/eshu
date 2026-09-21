// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsMutationAndKeyMaterial is the metadata-only
// acceptance gate for CloudHSM v2: the SDK adapter must never mutate CloudHSM
// state, initialize a cluster (the flow that exposes the Pre-Crypto Officer
// password), restore/copy/delete a backup, or read a resource policy. We reflect
// over the adapter's read interface and confirm no mutation, initialize, or
// resource-policy method is reachable. This test fails the build if a future
// edit ever adds one of these to the adapter surface.
func TestAdapterInterfaceForbidsMutationAndKeyMaterial(t *testing.T) {
	forbiddenSubstrings := []string{
		// initializing a cluster is the flow that surfaces the PRECO password.
		"Initialize",
		// resource policy reads are not part of the metadata contract.
		"ResourcePolicy", "Policy",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Modify", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore", "Copy", "Initialize", "Get",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the CloudHSM v2 read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden method %q; the CloudHSM v2 adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the CloudHSM v2 adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreDescribeReads asserts every method on the adapter
// interface is a Describe read so the read surface stays explicit and auditable.
// The scanner reads cluster and backup control-plane metadata only; nothing
// fetches key material, certificate bodies, or a resource policy.
func TestAdapterMethodsAreDescribeReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is not a Describe read", name)
		}
	}
}
