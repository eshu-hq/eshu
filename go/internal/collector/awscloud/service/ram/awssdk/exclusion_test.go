// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterAPIClientForbidsMutationAndPolicyBody is the security acceptance
// gate from issue #833: the RAM SDK adapter must never be able to create,
// delete, update, associate, disassociate, accept, reject, enable, disable,
// promote, replace, tag, untag, or set a default permission version on any RAM
// resource, and it must never read a permission policy document body via
// GetPermission. We reflect over the adapter-local apiClient interface and fail
// the build if any forbidden operation becomes reachable.
func TestAdapterAPIClientForbidsMutationAndPolicyBody(t *testing.T) {
	forbiddenExact := []string{
		// GetPermission and GetPermissionVersion return the permission policy
		// document body; the metadata-only adapter must never reach them.
		"GetPermission", "GetPermissionVersion",
	}
	// Any method whose name begins with one of these verbs is a write, lifecycle,
	// invitation, or promotion operation and must not exist on the metadata-only
	// adapter.
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put",
		"Associate", "Disassociate",
		"Accept", "Reject",
		"Enable", "Disable",
		"Promote", "Replace",
		"Tag", "Untag",
		"SetDefault",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		for _, banned := range forbiddenExact {
			if name == banned {
				t.Fatalf("apiClient exposes forbidden method %q; the RAM adapter is metadata-only and never reads a permission policy body", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the RAM adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreReadOnly asserts every method on the apiClient interface
// is a Get or List read so the read surface stays explicit and auditable.
func TestAdapterMethodsAreReadOnly(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	if iface.NumMethod() == 0 {
		t.Fatalf("apiClient interface has no methods; expected the RAM read surface")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if !strings.HasPrefix(name, "Get") && !strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is neither a Get nor List read", name)
		}
	}
}
