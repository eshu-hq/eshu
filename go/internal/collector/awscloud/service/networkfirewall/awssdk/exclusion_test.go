// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// forbiddenNetworkFirewallOperations are the Network Firewall mutation and
// sensitive-body read methods the scanner SDK adapter must never be able to
// call. Any one of them on the apiClient interface would let a future edit
// mutate firewall state or read rule group rule sources (Suricata signature
// bodies). Keeping the list here makes the security contract a compile-and-test
// gate rather than a review convention.
//
// DescribeRuleGroup is forbidden on purpose: its DescribeRuleGroupOutput.RuleGroup
// field carries the rule source. The adapter reads rule group metadata through
// DescribeRuleGroupMetadata, which never returns rule bodies.
var forbiddenNetworkFirewallOperations = []string{
	"CreateFirewall",
	"UpdateFirewallDeleteProtection",
	"UpdateFirewallDescription",
	"UpdateFirewallEncryptionConfiguration",
	"UpdateFirewallPolicyChangeProtection",
	"UpdateSubnetChangeProtection",
	"UpdateAvailabilityZoneChangeProtection",
	"DeleteFirewall",
	"AssociateFirewallPolicy",
	"AssociateSubnets",
	"DisassociateSubnets",
	"AssociateAvailabilityZones",
	"DisassociateAvailabilityZones",
	"CreateFirewallPolicy",
	"UpdateFirewallPolicy",
	"DeleteFirewallPolicy",
	"CreateRuleGroup",
	"UpdateRuleGroup",
	"DeleteRuleGroup",
	"DescribeRuleGroup",
	"DescribeRuleGroupSummary",
	"CreateTLSInspectionConfiguration",
	"UpdateTLSInspectionConfiguration",
	"DeleteTLSInspectionConfiguration",
	"PutResourcePolicy",
	"DeleteResourcePolicy",
	"DescribeResourcePolicy",
	"UpdateLoggingConfiguration",
	"DescribeLoggingConfiguration",
	"TagResource",
	"UntagResource",
	"StartAnalysisReport",
	"StartFlowCapture",
	"StartFlowFlush",
}

// TestAPIClientInterfaceExcludesMutationAndRuleBodyMethods asserts that the
// adapter's apiClient interface exposes none of the forbidden Network Firewall
// operations. The reflection check fails the build path the moment a mutation
// or rule-body read method is added to the interface, before any runtime call
// can reach AWS.
func TestAPIClientInterfaceExcludesMutationAndRuleBodyMethods(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	present := make(map[string]struct{}, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		present[iface.Method(i).Name] = struct{}{}
	}
	for _, forbidden := range forbiddenNetworkFirewallOperations {
		if _, found := present[forbidden]; found {
			t.Fatalf("apiClient exposes forbidden Network Firewall operation %q", forbidden)
		}
	}
}

// TestAPIClientInterfaceUsesReadOnlyVerbs asserts that every method on the
// adapter interface is a read verb (List/Describe). It catches mutation methods
// whose names are not in forbiddenNetworkFirewallOperations.
func TestAPIClientInterfaceUsesReadOnlyVerbs(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is not a read-only List/Describe verb", name)
		}
	}
}
