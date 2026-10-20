// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsReplicationSecretsAndMutation is the metadata-only
// acceptance gate for MGN: the SDK adapter must never read replication-agent
// credentials or replication configuration secrets and must never write MGN
// state. We reflect over the adapter's read interface and confirm no
// replication-configuration read, replication-template read, or mutation method
// is reachable. GetReplicationConfiguration (which carries staging credentials)
// is excluded; every Start/Stop/Terminate/Create/Update/Delete launch and
// replication control API is excluded by prefix. This test fails the build if a
// future edit ever adds one of these to the adapter surface.
func TestAdapterInterfaceForbidsReplicationSecretsAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// replication configuration / template reads carry staging credentials.
		"ReplicationConfiguration", "ReplicationConfigurationTemplate",
		// agent / connector secret material.
		"Agent", "Connector", "Credential", "Secret", "Token",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Start", "Stop", "Terminate",
		"Mark", "Change", "Initialize", "Finalize", "Disconnect", "Archive",
		"Unarchive", "Associate", "Disassociate", "Add", "Remove", "Pause",
		"Resume", "Retry", "Launch", "Import", "Export", "Register",
		"Deregister", "Tag", "Untag", "Send",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the MGN read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden replication/secret method %q; the MGN adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the MGN adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreControlPlaneReads asserts every method on the adapter
// interface is a Describe/List/Get control-plane read so the read surface stays
// explicit and auditable.
func TestAdapterMethodsAreControlPlaneReads(t *testing.T) {
	allowedPrefixes := []string{"Describe", "List", "Get"}
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		ok := false
		for _, prefix := range allowedPrefixes {
			if strings.HasPrefix(name, prefix) {
				ok = true
				break
			}
		}
		if !ok {
			t.Fatalf("apiClient method %q is not a Describe/List/Get read", name)
		}
	}
}
