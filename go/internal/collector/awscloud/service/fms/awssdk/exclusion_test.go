// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// forbiddenFMSOperations are the Firewall Manager mutation and rule-payload
// read methods the scanner SDK adapter must never be able to call. Any one of
// them on the apiClient interface would let a future edit mutate FMS state or
// read a policy rule payload (the SecurityServicePolicyData managed service data
// document). GetPolicy is forbidden because it returns the full Policy with the
// rule payload; ListPolicies already returns every metadata field the scanner
// records, so the rule body is unreachable by construction. Keeping the list
// here makes the security contract a compile-and-test gate rather than a review
// convention.
var forbiddenFMSOperations = []string{
	"PutPolicy",
	"DeletePolicy",
	"GetPolicy",
	"PutNotificationChannel",
	"DeleteNotificationChannel",
	"AssociateAdminAccount",
	"DisassociateAdminAccount",
	"PutAdminAccount",
	"DeleteAppsList",
	"PutAppsList",
	"DeleteProtocolsList",
	"PutProtocolsList",
	"DeleteResourceSet",
	"PutResourceSet",
	"BatchAssociateResource",
	"BatchDisassociateResource",
	"AssociateThirdPartyFirewall",
	"DisassociateThirdPartyFirewall",
	"TagResource",
	"UntagResource",
}

// TestAPIClientInterfaceExcludesMutationAndRulePayloadMethods asserts that the
// adapter's apiClient interface exposes none of the forbidden FMS operations.
// The reflection check fails the build path the moment a mutation or
// rule-payload read method is added to the interface, before any runtime call
// can reach AWS.
func TestAPIClientInterfaceExcludesMutationAndRulePayloadMethods(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	present := make(map[string]struct{}, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		present[iface.Method(i).Name] = struct{}{}
	}
	for _, forbidden := range forbiddenFMSOperations {
		if _, found := present[forbidden]; found {
			t.Fatalf("apiClient exposes forbidden FMS operation %q", forbidden)
		}
	}
}

// TestAPIClientInterfaceUsesReadOnlyVerbs asserts that every method on the
// adapter interface is a List verb. FMS list APIs return only metadata; the
// scanner deliberately avoids the Get* reads that surface rule payloads. The
// check catches mutation methods whose names are not in
// forbiddenFMSOperations.
func TestAPIClientInterfaceUsesReadOnlyVerbs(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if !strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is not a read-only List verb", name)
		}
	}
}
