// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsSessionAndMutation is the metadata-only acceptance
// gate for WorkSpaces: the SDK adapter must never read desktop session
// contents, connection state, or credentials, and must never create, modify,
// reboot, rebuild, start, stop, or terminate a WorkSpace or any WorkSpaces
// resource. We reflect over the adapter's read interface and confirm no session,
// connection-status, credential, or mutation method is reachable. This test
// fails the build if a future edit ever adds one of these to the adapter
// surface.
func TestAdapterInterfaceForbidsSessionAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// session / connection / credential reads — never reachable.
		"ConnectionStatus", "ConnectionAlias", "PoolSession", "Session",
		"ClientProperties", "ClientBranding", "Snapshot", "Password",
		"Credential", "ImagePermissions",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Modify", "Update", "Put", "Reboot", "Rebuild",
		"Start", "Stop", "Terminate", "Restore", "Migrate", "Associate",
		"Disassociate", "Authorize", "Revoke", "Register", "Deregister",
		"Import", "Copy", "Accept", "Reject", "Add", "Remove", "Apply",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the WorkSpaces read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden session/mutation method %q; the WorkSpaces adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the WorkSpaces adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreDescribeReads asserts every method on the adapter
// interface is a Describe read so the read surface stays explicit and
// auditable. The scanner reads WorkSpaces, directory, bundle, and IP-group
// metadata and resource tags only.
func TestAdapterMethodsAreDescribeReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is not a Describe read", name)
		}
	}
}
