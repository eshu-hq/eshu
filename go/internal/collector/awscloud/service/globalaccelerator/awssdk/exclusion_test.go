// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterAPIClientForbidsMutation is the security acceptance gate from
// issue #835: the Global Accelerator SDK adapter must never be able to create,
// update, delete, provision, advertise, or otherwise mutate any accelerator,
// listener, endpoint group, endpoint, or BYOIP CIDR. We reflect over the
// adapter-local apiClient interface and fail the build if any forbidden
// operation becomes reachable.
func TestAdapterAPIClientForbidsMutation(t *testing.T) {
	forbiddenExact := []string{
		"AddCustomRoutingEndpoints", "RemoveCustomRoutingEndpoints",
		"AddEndpoints", "RemoveEndpoint",
		"AdvertiseByoipCidr", "WithdrawByoipCidr",
		"AllowCustomRoutingTraffic", "DenyCustomRoutingTraffic",
	}
	// Any method whose name begins with one of these verbs is a write or
	// lifecycle operation and must not exist on the metadata-only adapter.
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put",
		"Add", "Remove", "Provision", "Deprovision",
		"Advertise", "Withdraw", "Allow", "Deny",
		"Tag", "Untag",
		"Associate", "Disassociate",
		"Enable", "Disable", "Start", "Stop",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		for _, banned := range forbiddenExact {
			if name == banned {
				t.Fatalf("apiClient exposes forbidden mutation method %q; the Global Accelerator adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Global Accelerator adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreReadOnly asserts every method on the apiClient interface
// is a List read so the read surface stays explicit and auditable. Global
// Accelerator exposes its topology entirely through List operations, so a
// Describe is not needed.
func TestAdapterMethodsAreReadOnly(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	if iface.NumMethod() == 0 {
		t.Fatalf("apiClient interface has no methods; expected the Global Accelerator read surface")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is neither a List nor Describe read", name)
		}
	}
}
