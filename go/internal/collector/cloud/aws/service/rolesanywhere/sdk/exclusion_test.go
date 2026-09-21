// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsBodiesAndMutation is the metadata-only acceptance
// gate the issue calls out for Roles Anywhere: the SDK adapter must never read
// certificate private material, CRL body bytes, or vended session credentials,
// and must never mutate Roles Anywhere state. We reflect over the adapter's read
// interface and confirm no body-read, subject/credential-read, or mutation
// method is reachable. GetCrl returns the CRL body bytes; GetSubject and
// ListSubjects expose vended session credentials; both are excluded. This test
// fails the build if a future edit ever adds one of these to the adapter
// surface.
func TestAdapterInterfaceForbidsBodiesAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// data-plane / body / credential reads — never reachable.
		"GetCrl", "Subject", "Credential", "Session", "Certificate", "CrlData",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister", "Reset",
		"Associate", "Disassociate", "Send", "Import", "Export",
		"Tag", "Untag", "Resume", "Restore", "Get",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Roles Anywhere read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden body/credential method %q; the Roles Anywhere adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation/get method %q (prefix %q); the Roles Anywhere adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListReads asserts every method on the adapter interface
// is a List read so the read surface stays explicit and auditable. The scanner
// reads trust-anchor, profile, and CRL metadata plus resource tags only; nothing
// fetches a CRL body, certificate material, or session credentials.
func TestAdapterMethodsAreListReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is not a List read", name)
		}
	}
}
