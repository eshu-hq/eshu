// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsJobsAndMutation is the metadata-only acceptance
// gate for Signer: the SDK adapter must never start a signing job, read signing
// material private keys, read signed-object payloads, or mutate Signer state. We
// reflect over the adapter's read interface and confirm no signing-job read,
// sign, or mutation method is reachable. This test fails the build if a future
// edit ever adds one of these to the adapter surface.
func TestAdapterInterfaceForbidsJobsAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// signing-job, payload, permission, and revocation reads — never reachable.
		"SigningJob", "SigningJobs", "RevocationStatus", "Payload", "Permission",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore", "Cancel", "Revoke", "Describe",
		// "Sign" as a prefix is a signing action (SignPayload); "Signing" as a
		// noun prefix (e.g. SigningProfiles) is a legitimate metadata read and is
		// allowed only through the List/Get gate in the companion test.
		"SignP", "Signature",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Signer read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden job/mutation method %q; the Signer adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Signer adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrGetReads asserts every method on the adapter
// interface is a List or Get read so the read surface stays explicit and
// auditable. The scanner reads signing-profile and signing-platform metadata
// only; nothing describes jobs or fetches signed payloads.
func TestAdapterMethodsAreListOrGetReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is not a List or Get read", name)
		}
	}
}
