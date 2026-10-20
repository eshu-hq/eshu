// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// forbiddenShieldOperations are the Shield Advanced mutation and engagement
// methods the scanner SDK adapter must never be able to call. Any one of them
// on the apiClient interface would let a future edit create, delete, or
// reconfigure Shield protections or subscriptions. Keeping the list here makes
// the metadata-only security contract a compile-and-test gate rather than a
// review convention.
var forbiddenShieldOperations = []string{
	"CreateProtection",
	"DeleteProtection",
	"UpdateProtection",
	"CreateProtectionGroup",
	"DeleteProtectionGroup",
	"UpdateProtectionGroup",
	"AssociateDRTLogBucket",
	"AssociateDRTRole",
	"AssociateHealthCheck",
	"DisassociateHealthCheck",
	"AssociateProactiveEngagementDetails",
	"CreateSubscription",
	"DeleteSubscription",
	"UpdateSubscription",
	"UpdateApplicationLayerAutomaticResponse",
	"EnableApplicationLayerAutomaticResponse",
	"DisableApplicationLayerAutomaticResponse",
	"UpdateEmergencyContactSettings",
	"EnableProactiveEngagement",
	"DisableProactiveEngagement",
	"UpdateSubscriptionLimits",
	"TagResource",
	"UntagResource",
}

// TestAPIClientInterfaceExcludesMutationMethods asserts that the adapter's
// apiClient interface exposes none of the forbidden Shield operations. The
// reflection check fails the build path the moment a mutation method is added to
// the interface, before any runtime call can reach AWS.
func TestAPIClientInterfaceExcludesMutationMethods(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	present := make(map[string]struct{}, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		present[iface.Method(i).Name] = struct{}{}
	}
	for _, forbidden := range forbiddenShieldOperations {
		if _, found := present[forbidden]; found {
			t.Fatalf("apiClient exposes forbidden Shield operation %q", forbidden)
		}
	}
}

// TestAPIClientInterfaceUsesReadOnlyVerbs asserts every method on the adapter
// interface is a read verb (List/Describe/Get). It catches mutation methods
// whose names are not in forbiddenShieldOperations.
func TestAPIClientInterfaceUsesReadOnlyVerbs(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if !strings.HasPrefix(name, "List") &&
			!strings.HasPrefix(name, "Describe") &&
			!strings.HasPrefix(name, "Get") {
			t.Fatalf("apiClient method %q is not a read-only List/Describe/Get verb", name)
		}
	}
}
