// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docdb

import (
	"reflect"
	"strings"
	"testing"
)

// TestClientInterfaceExcludesMutationAndDataPlaneAPIs asserts the Client
// surface in this package only exposes the metadata-only reads listed in the
// DocumentDB scanner contract. Issue #736 forbids the scanner from calling any
// mutation API (Create/Delete/Modify DBCluster/DBInstance/
// DBClusterParameterGroup/DBSubnetGroup, RestoreDBClusterFromSnapshot,
// CreateDBClusterSnapshot, DeleteDBClusterSnapshot, ModifyDBCluster,
// FailoverDBCluster, RebootDBInstance) or reaching database document contents.
// The Client interface is the only way the scanner reaches the DocumentDB API,
// so asserting the interface shape proves those APIs are unreachable from this
// code path.
func TestClientInterfaceExcludesMutationAndDataPlaneAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	want := map[string]bool{
		"ListDBClusters":             true,
		"ListClusterInstances":       true,
		"ListClusterParameterGroups": true,
		"ListClusterSnapshots":       true,
		"ListSubnetGroups":           true,
		"ListGlobalClusters":         true,
		"ListEventSubscriptions":     true,
	}
	have := map[string]bool{}
	for i := 0; i < clientType.NumMethod(); i++ {
		have[clientType.Method(i).Name] = true
	}
	for name := range want {
		if !have[name] {
			t.Errorf("Client interface missing required method %q", name)
		}
	}
	for name := range have {
		if !want[name] {
			t.Errorf("Client interface exposes unexpected method %q; metadata-only contract violated", name)
		}
	}

	// Defensive check: any method name containing a forbidden mutation,
	// restore, failover, reboot, snapshot-write, or data-plane verb is a
	// contract violation. The list mirrors issue #736 acceptance language.
	forbiddenSubstrings := []string{
		"Create",
		"Delete",
		"Modify",
		"Restore",
		"Failover",
		"Reboot",
		"Start",
		"Stop",
		"Add",
		"Remove",
		"Copy",
		"Apply",
		"Reset",
		"Query",
		"Find",
		"Insert",
		"Update",
		"Write",
	}
	for name := range have {
		for _, forbidden := range forbiddenSubstrings {
			if strings.Contains(name, forbidden) {
				t.Errorf("Client method %q contains forbidden substring %q", name, forbidden)
			}
		}
	}
}
