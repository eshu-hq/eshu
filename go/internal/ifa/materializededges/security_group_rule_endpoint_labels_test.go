// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
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
