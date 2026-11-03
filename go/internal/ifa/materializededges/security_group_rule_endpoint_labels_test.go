// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckEndpointTargetLabelsBindsLabelPerRule proves the hermetic guard
// pins the per-edge label binding (#6228 review P2): the shared fixture
// compares edge identity alone, so a regression resolving the right uid
// under the wrong label must fail here, not pass through to the live tier.
func TestCheckEndpointTargetLabelsBindsLabelPerRule(t *testing.T) {
	t.Parallel()

	matching := []map[string]any{}
	for ruleUID, label := range securityGroupRuleEndpointTargetLabelByRule {
		matching = append(matching, map[string]any{
			"rule_uid":     ruleUID,
			"target_label": label,
		})
	}
	if got := checkSecurityGroupRuleEndpointTargetLabels("probe", matching); got != "" {
		t.Fatalf("matching labels = %q, want clean", got)
	}

	swapped := []map[string]any{{
		"rule_uid":     "0a3afef6f922fb2707a353db2d131190432d0120fe17988f3b5d0a69768c8161",
		"target_label": "PrefixList",
	}}
	if got := checkSecurityGroupRuleEndpointTargetLabels("probe", swapped); !strings.Contains(got, "want \"CidrBlock\"") {
		t.Fatalf("swapped label = %q, want a want-CidrBlock failure", got)
	}

	unknown := []map[string]any{{
		"rule_uid":     "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		"target_label": "CidrBlock",
	}}
	if got := checkSecurityGroupRuleEndpointTargetLabels("probe", unknown); !strings.Contains(got, "unlisted rule_uid") {
		t.Fatalf("unknown rule = %q, want a fail-closed unlisted-rule failure", got)
	}
}

// TestEndpointGuardInvokesTargetLabelCheck pins the wiring the cases above
// cannot reach: the helper is only real if the guard calls it. A swapped
// label with a known rule_uid is unreachable through the production
// extractor (the uid folds the same source tokens the label derives from),
// so no Odu-level input distinguishes "call present" from "call deleted" —
// deleting the call-site line below must fail this test instead.
func TestEndpointGuardInvokesTargetLabelCheck(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join(repoRootDir(t), "go", "internal", "ifa", "materializededges", "security_group_rule_endpoint.go"))
	if err != nil {
		t.Fatalf("read guard source: %v", err)
	}
	const call = "checkSecurityGroupRuleEndpointTargetLabels(odu.Name, result.RuleEndpointEdges)"
	if !strings.Contains(string(src), call) {
		t.Fatalf("guard no longer invokes %s; the label regression cases above pass while swapped-label rows flow unchecked", call)
	}
}
