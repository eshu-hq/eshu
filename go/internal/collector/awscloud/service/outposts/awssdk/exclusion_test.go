// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsMutationAndAddressReads is the metadata-only
// acceptance gate for Outposts: the SDK adapter must never mutate Outposts state
// and must never read physical site street addresses, shipping or contact
// details, orders, billing, or connection data. We reflect over the adapter's
// read interface and confirm no mutation method and no address/order/billing/
// connection read is reachable. This test fails the build if a future edit ever
// adds one of these to the adapter surface.
func TestAdapterInterfaceForbidsMutationAndAddressReads(t *testing.T) {
	forbiddenSubstrings := []string{
		// physical site street address / logistics reads — never reachable.
		"Address", "Order", "Billing", "Connection", "Catalog",
		// pricing/renewal/capacity-task reads carry logistics, not metadata.
		"Pricing", "Renewal", "CapacityTask", "BlockingInstances",
		// instance-type capacity inventory is not operational identity.
		"InstanceTypes", "SupportedInstanceTypes", "AssetInstances",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister", "Cancel",
		"Associate", "Disassociate", "Send", "Import", "Tag", "Untag",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Outposts read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden method %q (substring %q); the Outposts adapter is metadata-only and never reads addresses, orders, or logistics", name, banned)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Outposts adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrGetReads asserts every method on the adapter
// interface is a List or Get read so the read surface stays explicit and
// auditable. The scanner reads outpost, site, and asset metadata and resource
// tags only.
func TestAdapterMethodsAreListOrGetReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is not a List or Get read", name)
		}
	}
}
