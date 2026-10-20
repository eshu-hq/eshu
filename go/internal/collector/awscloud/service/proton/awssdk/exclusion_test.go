// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsMutationAndBodies is the metadata-only acceptance
// gate for Proton: the SDK adapter must never mutate Proton state and must never
// reach a spec/template-schema body or deployment output reader. We reflect over
// the adapter's read interface and confirm no mutation, sync, or
// outputs/provisioned-resources reader is reachable. The accepted surface is the
// environment/service/template list reads, the GetService detail read (mapped to
// reference-only repository linkage), the service-instance list read, and the
// resource-tag read. This test fails the build if a future edit ever adds a
// mutation or body/output reader to the adapter surface.
func TestAdapterInterfaceForbidsMutationAndBodies(t *testing.T) {
	forbiddenSubstrings := []string{
		// deployment output / provisioned-resource readers expose values.
		"Outputs", "ProvisionedResources",
		// sync status / config readers and component readers are out of scope.
		"SyncStatus", "SyncConfig", "SyncBlocker", "Component",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Cancel", "Reject", "Accept",
		"Notify", "Tag", "Untag", "Add", "Remove", "Start", "Stop",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Proton read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden reader/mutation method %q; the Proton adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Proton adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrGetService asserts every adapter method is either a
// List read or the single allowed GetService detail read, so the read surface
// stays explicit and auditable. GetService is the only Get call, and the mapper
// reads only its reference fields, never the service Spec body.
func TestAdapterMethodsAreListOrGetService(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if strings.HasPrefix(name, "List") {
			continue
		}
		if name == "GetService" {
			continue
		}
		t.Fatalf("apiClient method %q is neither a List read nor the allowed GetService detail read", name)
	}
}
