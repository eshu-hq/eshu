// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsConfigContentAndMutation is the metadata-only
// acceptance gate for AppConfig: the SDK adapter must never read configuration
// content (the freeform/feature-flag values an application distributes) and
// must never write AppConfig state or start a deployment. We reflect over the
// adapter's read interface and confirm no configuration-read, deployment-start,
// or mutation method is reachable. GetConfiguration and GetLatestConfiguration
// live in the separate appconfigdata module this package never imports, so they
// cannot appear at all; GetHostedConfigurationVersion (the stored configuration
// body) is excluded from the interface below. This test fails the build if a
// future edit ever adds one of these to the adapter surface.
func TestAdapterInterfaceForbidsConfigContentAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// configuration content reads — never reachable. These never appear in
		// the accepted ListApplications/ListEnvironments/
		// ListConfigurationProfiles/ListDeploymentStrategies surface.
		"HostedConfigurationVersion", "LatestConfiguration",
		"GetConfiguration", "Account",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Import", "Validate",
		"Tag", "Untag", "Get",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the AppConfig read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden content/mutation method %q; the AppConfig adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the AppConfig adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListReads asserts every method on the adapter interface
// is a List read so the read surface stays explicit and auditable. The scanner
// reads application, environment, configuration-profile, and deployment-strategy
// identity metadata only; nothing describes or fetches configuration content.
func TestAdapterMethodsAreListReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is not a List read", name)
		}
	}
}
