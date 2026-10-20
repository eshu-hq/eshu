// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsStateAndMutation is the metadata-only acceptance
// gate for Route 53 Application Recovery Controller: the SDK adapter must never
// read or set routing control state and must never mutate a recovery-control
// configuration resource. We reflect over the adapter's read interface and
// confirm no state-read, state-update, or mutation method is reachable.
// UpdateRoutingControlState lives only in the separate route53recoverycluster
// data-plane module this package never imports, so it cannot appear at all. This
// test fails the build if a future edit ever adds one of these to the adapter
// surface.
func TestAdapterInterfaceForbidsStateAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// routing control state reads/writes — never reachable.
		"RoutingControlState", "ControlState", "GetRoutingControl",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore", "Get", "Set",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the recovery-control read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf(
					"apiClient exposes forbidden state/mutation method %q; the recovery-control adapter is metadata-only",
					name,
				)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf(
					"apiClient exposes mutation method %q (prefix %q); the recovery-control adapter is metadata-only",
					name, prefix,
				)
			}
		}
	}
}

// TestAdapterMethodsAreListReads asserts every method on the adapter interface
// is a List read so the read surface stays explicit and auditable. The scanner
// reads cluster, control-panel, routing-control, and safety-rule metadata plus
// resource tags only; nothing describes or fetches routing control state.
func TestAdapterMethodsAreListReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is not a List read", name)
		}
	}
}
