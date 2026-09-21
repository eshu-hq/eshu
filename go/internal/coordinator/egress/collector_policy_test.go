// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package egress

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestParseCollectorEgressPolicyJSONEvaluatesRestrictedRules(t *testing.T) {
	t.Parallel()

	policy, err := ParseCollectorPolicyJSON(`{
		"mode": "restricted",
		"collectors": [
			{"collector_kind": "jira", "decision": "allow"},
			{"collector_kind": "pagerduty", "decision": "deny"}
		]
	}`)
	if err != nil {
		t.Fatalf("ParseCollectorPolicyJSON() error = %v, want nil", err)
	}

	decision := policy.Decide(scope.CollectorJira)
	if got, want := decision.Action, CollectorActionAllow; got != want {
		t.Fatalf("jira action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, CollectorReasonAllowed; got != want {
		t.Fatalf("jira reason = %q, want %q", got, want)
	}

	decision = policy.Decide(scope.CollectorPagerDuty)
	if got, want := decision.Action, CollectorActionDeny; got != want {
		t.Fatalf("pagerduty action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, CollectorReasonDenied; got != want {
		t.Fatalf("pagerduty reason = %q, want %q", got, want)
	}

	decision = policy.Decide(scope.CollectorAWS)
	if got, want := decision.Action, CollectorActionDeny; got != want {
		t.Fatalf("aws action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, CollectorReasonMissing; got != want {
		t.Fatalf("aws reason = %q, want %q", got, want)
	}
}

func TestCollectorEgressPolicyDeniesWinOverAllow(t *testing.T) {
	t.Parallel()

	policy, err := ParseCollectorPolicyJSON(`{
		"mode": "restricted",
		"collectors": [
			{"collector_kind": "pagerduty", "decision": "allow"},
			{"collector_kind": "pagerduty", "decision": "deny"}
		]
	}`)
	if err != nil {
		t.Fatalf("ParseCollectorPolicyJSON() error = %v, want nil", err)
	}

	decision := policy.Decide(scope.CollectorPagerDuty)
	if got, want := decision.Action, CollectorActionDeny; got != want {
		t.Fatalf("pagerduty action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, CollectorReasonDenied; got != want {
		t.Fatalf("pagerduty reason = %q, want %q", got, want)
	}
}

func TestCollectorEgressPolicyBroadModeRequiresEmptyRules(t *testing.T) {
	t.Parallel()

	policy, err := ParseCollectorPolicyJSON(`{"mode":"broad"}`)
	if err != nil {
		t.Fatalf("ParseCollectorPolicyJSON() error = %v, want nil", err)
	}
	decision := policy.Decide(scope.CollectorPagerDuty)
	if got, want := decision.Action, CollectorActionAllow; got != want {
		t.Fatalf("pagerduty action = %q, want %q", got, want)
	}

	_, err = ParseCollectorPolicyJSON(`{
		"mode": "broad",
		"collectors": [{"collector_kind": "pagerduty", "decision": "deny"}]
	}`)
	if err == nil {
		t.Fatal("ParseCollectorPolicyJSON() error = nil, want broad-mode rule rejection")
	}
	if got, want := err.Error(), "broad collector egress policy must not include collector-specific rules"; !strings.Contains(got, want) {
		t.Fatalf("ParseCollectorPolicyJSON() error = %q, want %q", got, want)
	}
}
