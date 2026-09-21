// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsMutation is the metadata-only acceptance gate for
// the Service Quotas scanner: the SDK adapter must never request, modify, or
// delete a quota, and must never associate or template a quota-increase. We
// reflect over the adapter's read interface and confirm no mutation method is
// reachable. RequestServiceQuotaIncrease, every template association, and every
// Put/Delete mutation live on the servicequotas client but are excluded from
// the interface below. This test fails the build if a future edit ever adds one
// of these to the adapter surface.
func TestAdapterInterfaceForbidsMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// quota-change request surfaces are writes, even though they "request".
		"Request", "Template", "ChangeHistory",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore", "Get",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Service Quotas read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden mutation/history method %q; the Service Quotas adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Service Quotas adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListReads asserts every method on the adapter interface
// is a List read so the read surface stays explicit and auditable. The scanner
// reads visible services, applied quotas, and AWS default quotas only; nothing
// reads or changes quota-increase request state.
func TestAdapterMethodsAreListReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is not a List read", name)
		}
	}
}
