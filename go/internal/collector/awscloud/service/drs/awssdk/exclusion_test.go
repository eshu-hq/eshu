// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsDataPlaneAndMutation is the metadata-only
// acceptance gate for DRS: the SDK adapter must never read replication agent
// secrets, replicated disk data, point-in-time snapshot contents, or job logs,
// and must never recover, start, stop, or mutate DRS state. We reflect over the
// adapter's read interface and confirm no data-plane read, agent read, or
// mutation method is reachable. This test fails the build if a future edit ever
// adds one of these to the adapter surface.
func TestAdapterInterfaceForbidsDataPlaneAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// data-plane / agent / job-log reads — never reachable. Note: the
		// account-level replication-configuration TEMPLATE describe is a
		// legitimate metadata read; the dangerous per-server live config lives
		// behind Get/Update prefixes, already covered by forbiddenPrefixes.
		"Snapshot", "Snapshots", "JobLog", "JobLogItems", "Logs",
		"Agent", "Secret", "Failback",
		"ExtensibleSourceServers", "StagingAccounts", "LaunchActions",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore", "Recover", "Reverse",
		"Terminate", "Initialize", "Retry", "Get",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the DRS describe read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, strings.TrimSpace(banned)) {
				t.Fatalf("apiClient exposes forbidden data-plane/mutation method %q; the DRS adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the DRS adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreDescribeReads asserts every method on the adapter
// interface is a Describe read so the read surface stays explicit and auditable.
// The scanner reads source server, recovery instance, and replication
// configuration template metadata only; nothing fetches agent or snapshot
// payloads.
func TestAdapterMethodsAreDescribeReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is not a Describe read", name)
		}
	}
}
