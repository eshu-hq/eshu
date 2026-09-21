// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsBodiesAndMutation is the metadata-only acceptance
// gate the issue calls out for Verified Permissions: the SDK adapter must never
// read Cedar policy statement bodies, schema bodies, or policy template bodies,
// must never evaluate an authorization request, and must never mutate Verified
// Permissions state. We reflect over the adapter's read interface and confirm
// no body-read, authorization, or mutation method is reachable. GetPolicy
// returns the Cedar statement, GetSchema returns the schema body, and
// GetPolicyTemplate returns the template body; all are excluded from the
// interface below. IsAuthorized/BatchIsAuthorized authorization evaluation is
// excluded too. This test fails the build if a future edit ever adds one of
// these to the adapter surface.
func TestAdapterInterfaceForbidsBodiesAndMutation(t *testing.T) {
	// Exact method names whose body or decision payload must never be reachable.
	// GetPolicyStore is intentionally NOT here: it returns only store metadata
	// (validation mode, deletion protection, encryption label, Cedar version,
	// tags), never a Cedar body.
	forbiddenNames := map[string]bool{
		"GetPolicy":         true, // returns the Cedar policy statement body.
		"GetSchema":         true, // returns the Cedar schema body.
		"GetPolicyTemplate": true, // returns the policy template body.
	}
	forbiddenSubstrings := []string{
		// body / schema / template reads — never reachable.
		"Schema", "Template", "Statement", "Definition",
		// authorization evaluation is a data-plane decision surface.
		"IsAuthorized", "Authorize", "Token",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Verified Permissions read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if forbiddenNames[name] {
			t.Fatalf("apiClient exposes forbidden body-read method %q; the Verified Permissions adapter is metadata-only", name)
		}
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden body/authorization method %q; the Verified Permissions adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Verified Permissions adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrStoreReads asserts every method on the adapter
// interface is either a List read or the GetPolicyStore metadata read, so the
// read surface stays explicit and auditable. The scanner reads policy store,
// policy, and identity source metadata only; nothing fetches a Cedar body,
// schema, or template.
func TestAdapterMethodsAreListOrStoreReads(t *testing.T) {
	allowed := map[string]bool{
		"ListPolicyStores":    true,
		"GetPolicyStore":      true,
		"ListPolicies":        true,
		"ListIdentitySources": true,
	}
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !allowed[name] {
			t.Fatalf("apiClient method %q is not an allowed metadata read", name)
		}
	}
}
