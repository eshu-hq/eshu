// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterAPIClientForbidsMutation is the security acceptance gate from
// issue #838: the Route 53 Resolver SDK adapter must never be able to create,
// update, delete, associate, or disassociate any resolver or DNS Firewall
// resource. We reflect over the adapter-local apiClient interface and fail the
// build if any forbidden operation becomes reachable.
func TestAdapterAPIClientForbidsMutation(t *testing.T) {
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put",
		"Associate", "Disassociate",
		"Tag", "Untag",
		"Import", "Enable", "Disable", "Start", "Stop",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Route 53 Resolver adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterAPIClientNeverReadsDomainOrRuleContents is the privacy acceptance
// gate from issue #838: the adapter must never read DNS Firewall domain list
// contents (ListFirewallDomains) or DNS Firewall rule bodies
// (ListFirewallRules). Counts come from the per-resource Get reads instead.
// Query log records are read by no operation on the surface. We fail the build
// if any forbidden content reader becomes reachable.
func TestAdapterAPIClientNeverReadsDomainOrRuleContents(t *testing.T) {
	forbiddenExact := []string{
		"ListFirewallDomains",
		"ListFirewallRules",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		for _, banned := range forbiddenExact {
			if name == banned {
				t.Fatalf("apiClient exposes forbidden content reader %q; the Route 53 Resolver adapter never persists domain or rule contents", name)
			}
		}
	}
}

// TestAdapterMethodsAreReadOnly asserts every method on the apiClient interface
// is a List or Get read so the read surface stays explicit and auditable.
func TestAdapterMethodsAreReadOnly(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	if iface.NumMethod() == 0 {
		t.Fatalf("apiClient interface has no methods; expected the Route 53 Resolver read surface")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is neither a List nor Get read", name)
		}
	}
}
