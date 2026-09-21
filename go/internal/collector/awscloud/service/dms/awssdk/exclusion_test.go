// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsDataPlaneAndMutation is the metadata-only
// acceptance gate for DMS: the SDK adapter must never read migrated rows, test
// live endpoint connections, refresh schemas, reload tables, start/stop tasks,
// or mutate DMS state. We reflect over the adapter's read interface and confirm
// no data-plane, connection-test, schema-refresh, or mutation method is
// reachable. This test fails the build if a future edit ever adds one of these
// to the adapter surface.
func TestAdapterInterfaceForbidsDataPlaneAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// data-plane / live-connection reads — never reachable.
		"TestConnection", "RefreshSchemas", "ReloadTables", "ReloadReplicationTables",
		"TableStatistics", "ApplyPendingMaintenanceAction",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore", "Modify", "Move",
		"Cancel", "Run", "Reboot", "Test", "Reload", "Refresh", "Apply",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the DMS read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden data-plane/mutation method %q; the DMS adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the DMS adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreControlPlaneReads asserts every method on the adapter
// interface is a Describe or List read so the read surface stays explicit and
// auditable. The scanner reads instance, subnet group, endpoint, and task
// metadata plus resource tags only; nothing fetches migrated-row payloads.
func TestAdapterMethodsAreControlPlaneReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "Describe") && !strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is not a Describe or List read", name)
		}
	}
}
