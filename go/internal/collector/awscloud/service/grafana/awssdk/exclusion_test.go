// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsAuthSecretsAndMutation is the metadata-only
// acceptance gate the issue calls out for Managed Grafana: the SDK adapter must
// never read workspace authentication configuration (SAML / IAM Identity
// Center), never mint a workspace API key or service-account token, and never
// mutate workspace state. We reflect over the adapter's read interface and
// confirm no authentication-read, key/token, or mutation method is reachable.
// DescribeWorkspaceAuthentication and DescribeWorkspaceConfiguration are
// excluded from the interface below by the "Authentication"/"Configuration"
// substring guard; every Create/Update/Delete API is excluded by prefix. This
// test fails the build if a future edit adds one of these to the adapter
// surface.
func TestAdapterInterfaceForbidsAuthSecretsAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// authentication / configuration reads can surface SAML and IAM Identity
		// Center secrets — never reachable.
		"Authentication", "Configuration",
		// API keys and service-account tokens are credentials.
		"ApiKey", "Token", "ServiceAccount", "Permission",
		// license association is a mutation surface.
		"License",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Tag", "Untag", "Import",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the Grafana read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden method %q; the Grafana adapter is metadata-only and never reads auth secrets or mints keys/tokens", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Grafana adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListOrDescribeReads asserts every method on the adapter
// interface is a List or Describe read so the read surface stays explicit and
// auditable. The scanner reads workspace metadata and resource tags only.
func TestAdapterMethodsAreListOrDescribeReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is not a List or Describe read", name)
		}
	}
}
