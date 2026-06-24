// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package datasync

import (
	"reflect"
	"strings"
	"testing"
)

// TestClientInterfaceExcludesMutationAndUnsafeReadAPIs asserts the Client
// surface in this package only exposes the metadata-only reads the DataSync
// scanner contract allows. The Client interface is the only way the scanner
// reaches the AWS DataSync API, so asserting its shape is a load-bearing proof
// that transfer execution and resource mutation are unreachable from this code
// path: no CreateTask, StartTaskExecution, CancelTaskExecution, UpdateTask,
// DeleteTask, CreateLocation*, CreateAgent, UpdateAgent, or DeleteAgent method
// exists to call.
func TestClientInterfaceExcludesMutationAndUnsafeReadAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	want := map[string]bool{
		"ListTasks":     true,
		"ListLocations": true,
		"ListAgents":    true,
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

	// Defensive check: any method name containing a mutation or transfer-control
	// verb is a contract violation, even if the allow-set above were edited.
	forbiddenSubstrings := []string{
		"Create",
		"Update",
		"Delete",
		"Start",
		"Stop",
		"Cancel",
		"Put",
		"Add",
		"Remove",
		"Tag",
		"Untag",
		"Execution",
	}
	for name := range have {
		for _, forbidden := range forbiddenSubstrings {
			if strings.Contains(name, forbidden) {
				t.Errorf("Client method %q contains forbidden substring %q; metadata-only contract violated", name, forbidden)
			}
		}
	}
}
