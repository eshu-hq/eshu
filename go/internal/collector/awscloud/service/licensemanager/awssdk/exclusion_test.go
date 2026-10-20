// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsCheckoutAndMutation is the metadata-only acceptance
// gate for License Manager: the SDK adapter must never grant, check out, check
// in, or mutate a license and must never read a license entitlement token or
// usage record. We reflect over the adapter's read interface and confirm no
// checkout, entitlement-token, grant, or mutation method is reachable. This test
// fails the build if a future edit ever adds one of these to the adapter
// surface.
func TestAdapterInterfaceForbidsCheckoutAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// entitlement / checkout / token reads — never reachable.
		"Checkout", "CheckIn", "AccessToken", "GetLicense", "Entitlement",
		"Usage", "GrantedLicense", "ReceivedLicense",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Accept", "Reject",
		"Extend", "Renew", "Revoke", "Check", "Get",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the License Manager read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden checkout/entitlement method %q; the License Manager adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation/entitlement method %q (prefix %q); the License Manager adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListReads asserts every method on the adapter interface
// is a List read so the read surface stays explicit and auditable. The scanner
// reads license-configuration metadata, resource associations, and resource
// tags only; nothing checks out a license or reads an entitlement token.
func TestAdapterMethodsAreListReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is not a List read", name)
		}
	}
}
