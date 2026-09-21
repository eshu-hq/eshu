// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterInterfaceForbidsBodiesAndMutation is the metadata-only acceptance
// gate the issue calls out for AMP: the SDK adapter must never read ingested
// samples, rule-group definition bodies, alert-manager definitions, or
// scrape-configuration bodies, and must never mutate AMP state. We reflect over
// the adapter's read interface and confirm no describe-body, query, write, or
// mutation method is reachable. DescribeRuleGroupsNamespace (the rule body),
// DescribeWorkspaceConfiguration, GetDefaultScraperConfiguration, and every
// Create/Put/Delete/Update mutation are excluded from the interface below. This
// test fails the build if a future edit ever adds one of these to the adapter
// surface.
func TestAdapterInterfaceForbidsBodiesAndMutation(t *testing.T) {
	forbiddenSubstrings := []string{
		// rule / alert-manager / scrape-config / query body reads — never reachable.
		// "RuleGroup" is intentionally NOT banned: ListRuleGroupsNamespaces
		// returns namespace NAMES only, while the rule-definition body is read by
		// DescribeRuleGroupsNamespace, which the "Describe" substring below bans.
		"Describe", "Configuration", "AlertManager", "Definition",
		"Query", "Logging", "ResourcePolicy", "AnomalyDetector",
	}
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put", "Get", "Write", "Stop", "Start",
		"Add", "Disable", "Enable", "Register", "Deregister",
		"Associate", "Disassociate", "Send", "Batch", "Import",
		"Tag", "Untag", "Resume", "Restore",
	}

	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	if ifaceType.NumMethod() == 0 {
		t.Fatalf("apiClient has no methods; expected the AMP read surface")
	}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		for _, banned := range forbiddenSubstrings {
			if strings.Contains(name, banned) {
				t.Fatalf("apiClient exposes forbidden body/mutation method %q; the AMP adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the AMP adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreListReads asserts every method on the adapter interface
// is a List read so the read surface stays explicit and auditable. The scanner
// reads workspace, rule-groups namespace, and scraper metadata only; nothing
// describes or fetches rule, alert-manager, or scrape-configuration bodies.
func TestAdapterMethodsAreListReads(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < ifaceType.NumMethod(); i++ {
		name := ifaceType.Method(i).Name
		if !strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is not a List read", name)
		}
	}
}
