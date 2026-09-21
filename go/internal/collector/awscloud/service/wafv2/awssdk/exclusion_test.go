// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// forbiddenWAFv2Operations are the WAFv2 mutation and data-plane methods the
// scanner SDK adapter must never be able to call. Any one of them on the
// apiClient interface would let a future edit mutate WAF state or read
// sensitive bodies. Keeping the list here makes the security contract a
// compile-and-test gate rather than a review convention.
var forbiddenWAFv2Operations = []string{
	"CreateWebACL",
	"UpdateWebACL",
	"DeleteWebACL",
	"AssociateWebACL",
	"DisassociateWebACL",
	"CreateRuleGroup",
	"UpdateRuleGroup",
	"DeleteRuleGroup",
	"DeleteFirewallManagerRuleGroups",
	"CreateIPSet",
	"UpdateIPSet",
	"DeleteIPSet",
	"CreateRegexPatternSet",
	"UpdateRegexPatternSet",
	"DeleteRegexPatternSet",
	"PutLoggingConfiguration",
	"DeleteLoggingConfiguration",
	"PutManagedRuleSetVersions",
	"PutPermissionPolicy",
	"DeletePermissionPolicy",
	"TagResource",
	"UntagResource",
	"CreateAPIKey",
	"DeleteAPIKey",
	"GetDecryptedAPIKey",
	"GetSampledRequests",
}

// TestAPIClientInterfaceExcludesMutationAndDataPlaneMethods asserts that the
// adapter's apiClient interface exposes none of the forbidden WAFv2 operations.
// The reflection check fails the build path the moment a mutation method is
// added to the interface, before any runtime call can reach AWS.
func TestAPIClientInterfaceExcludesMutationAndDataPlaneMethods(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	present := make(map[string]struct{}, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		present[iface.Method(i).Name] = struct{}{}
	}
	for _, forbidden := range forbiddenWAFv2Operations {
		if _, found := present[forbidden]; found {
			t.Fatalf("apiClient exposes forbidden WAFv2 operation %q", forbidden)
		}
	}
}

// TestAPIClientInterfaceUsesReadOnlyVerbs asserts that every method on the
// adapter interface is a read verb (List/Get). It catches mutation methods
// whose names are not in forbiddenWAFv2Operations.
func TestAPIClientInterfaceUsesReadOnlyVerbs(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is not a read-only List/Get verb", name)
		}
	}
}
