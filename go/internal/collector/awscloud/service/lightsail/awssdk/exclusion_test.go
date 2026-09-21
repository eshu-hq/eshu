// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterAPIClientForbidsMutationAndSecretReads is the security acceptance
// gate from issue #887: the Lightsail SDK adapter must never be able to create,
// delete, reboot, start, stop, snapshot, attach, detach, or otherwise mutate a
// Lightsail resource, and must never read instance access keys, default
// key-pair private material, or database master passwords. We reflect over the
// adapter-local apiClient interface and fail the build if any forbidden
// operation becomes reachable.
func TestAdapterAPIClientForbidsMutationAndSecretReads(t *testing.T) {
	// Any method whose name begins with one of these verbs is a write,
	// lifecycle, or attach/detach operation and must not exist on the
	// metadata-only adapter.
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put",
		"Reboot", "Start", "Stop", "Restart",
		"Attach", "Detach", "Allocate", "Release",
		"Open", "Close", "Enable", "Disable",
		"Import", "Export", "Download", "Upload",
		"Tag", "Untag", "Set", "Peer", "Unpeer",
		"Send", "Reset", "Test",
	}
	// These exact reads expose secret or credential-bearing payloads and must
	// never be reachable even though they share the Get prefix of the safe
	// metadata readers.
	forbiddenExact := []string{
		"GetInstanceAccessDetails",
		"DownloadDefaultKeyPair",
		"GetKeyPair",
		"GetKeyPairs",
		"GetRelationalDatabaseMasterUserPassword",
		"GetInstanceState",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		for _, banned := range forbiddenExact {
			if name == banned {
				t.Fatalf("apiClient exposes forbidden secret/credential read %q; the Lightsail adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation/lifecycle method %q (prefix %q); the Lightsail adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreBoundedMetadataReads asserts the apiClient interface
// exposes exactly the five Get* list readers the Lightsail scanner needs, so the
// read surface stays explicit, auditable, and free of secret-bearing reads.
func TestAdapterMethodsAreBoundedMetadataReads(t *testing.T) {
	want := map[string]struct{}{
		"GetInstances":           {},
		"GetRelationalDatabases": {},
		"GetLoadBalancers":       {},
		"GetDisks":               {},
		"GetStaticIps":           {},
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	if got, wantN := iface.NumMethod(), len(want); got != wantN {
		t.Fatalf("apiClient method count = %d, want %d (the bounded Lightsail read surface)", got, wantN)
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if _, ok := want[name]; !ok {
			t.Fatalf("apiClient exposes unexpected method %q; only the five bounded Get* readers are allowed", name)
		}
		if !strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is not a Get read", name)
		}
	}
}
