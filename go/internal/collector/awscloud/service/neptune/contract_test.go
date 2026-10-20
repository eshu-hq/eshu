// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package neptune

import (
	"reflect"
	"strings"
	"testing"
)

// TestClientInterfaceExcludesMutationAndDataPlaneAPIs asserts the Client
// surface in this package only exposes the metadata-only reads listed in the
// Neptune scanner contract. Issue #737 forbids the scanner from calling any
// mutation API (Create/Delete/Modify DBCluster/DBInstance/
// DBClusterParameterGroup/DBSubnetGroup, RestoreDBClusterFromSnapshot,
// FailoverDBCluster, RebootDBInstance, CreateGraph, DeleteGraph, ResetGraph,
// UpdateGraph, RestoreGraphFromSnapshot, CreateGraphSnapshot) and any Neptune
// Analytics graph data-plane access (ExecuteQuery, CancelQuery, GetQuery,
// ListQueries, import/export tasks). The Client interface is the only way the
// scanner reaches the Neptune and Neptune Analytics APIs, so asserting the
// interface shape proves those APIs are unreachable from this code path.
func TestClientInterfaceExcludesMutationAndDataPlaneAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	want := map[string]bool{
		"ListDBClusters":             true,
		"ListClusterInstances":       true,
		"ListClusterParameterGroups": true,
		"ListClusterSnapshots":       true,
		"ListSubnetGroups":           true,
		"ListGlobalClusters":         true,
		"ListGraphs":                 true,
		"ListGraphSnapshots":         true,
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
	// restore, failover, reboot, snapshot-write, or graph data-plane verb is a
	// contract violation. The list mirrors issue #737 acceptance language and
	// the neptunegraph data-plane surface (ExecuteQuery/CancelQuery/GetQuery).
	forbiddenSubstrings := []string{
		"Create",
		"Delete",
		"Modify",
		"Restore",
		"Failover",
		"Reboot",
		"Start",
		"Stop",
		"Reset",
		"Update",
		"Add",
		"Remove",
		"Copy",
		"Apply",
		"Execute",
		"Query",
		"Cancel",
		"Import",
		"Export",
		"Load",
		"Unload",
		"Find",
		"Insert",
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
