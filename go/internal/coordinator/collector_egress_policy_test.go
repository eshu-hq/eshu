// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestParseCollectorEgressPolicyJSONEvaluatesRestrictedRules(t *testing.T) {
	t.Parallel()

	policy, err := ParseCollectorEgressPolicyJSON(`{
		"mode": "restricted",
		"collectors": [
			{"collector_kind": "jira", "decision": "allow"},
			{"collector_kind": "pagerduty", "decision": "deny"}
		]
	}`)
	if err != nil {
		t.Fatalf("ParseCollectorEgressPolicyJSON() error = %v, want nil", err)
	}

	decision := policy.Decide(scope.CollectorJira)
	if got, want := decision.Action, CollectorEgressActionAllow; got != want {
		t.Fatalf("jira action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, CollectorEgressReasonAllowed; got != want {
		t.Fatalf("jira reason = %q, want %q", got, want)
	}

	decision = policy.Decide(scope.CollectorPagerDuty)
	if got, want := decision.Action, CollectorEgressActionDeny; got != want {
		t.Fatalf("pagerduty action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, CollectorEgressReasonDenied; got != want {
		t.Fatalf("pagerduty reason = %q, want %q", got, want)
	}

	decision = policy.Decide(scope.CollectorAWS)
	if got, want := decision.Action, CollectorEgressActionDeny; got != want {
		t.Fatalf("aws action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, CollectorEgressReasonMissing; got != want {
		t.Fatalf("aws reason = %q, want %q", got, want)
	}
}

func TestCollectorEgressPolicyDeniesWinOverAllow(t *testing.T) {
	t.Parallel()

	policy, err := ParseCollectorEgressPolicyJSON(`{
		"mode": "restricted",
		"collectors": [
			{"collector_kind": "pagerduty", "decision": "allow"},
			{"collector_kind": "pagerduty", "decision": "deny"}
		]
	}`)
	if err != nil {
		t.Fatalf("ParseCollectorEgressPolicyJSON() error = %v, want nil", err)
	}

	decision := policy.Decide(scope.CollectorPagerDuty)
	if got, want := decision.Action, CollectorEgressActionDeny; got != want {
		t.Fatalf("pagerduty action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, CollectorEgressReasonDenied; got != want {
		t.Fatalf("pagerduty reason = %q, want %q", got, want)
	}
}

func TestCollectorEgressPolicyBroadModeRequiresEmptyRules(t *testing.T) {
	t.Parallel()

	policy, err := ParseCollectorEgressPolicyJSON(`{"mode":"broad"}`)
	if err != nil {
		t.Fatalf("ParseCollectorEgressPolicyJSON() error = %v, want nil", err)
	}
	decision := policy.Decide(scope.CollectorPagerDuty)
	if got, want := decision.Action, CollectorEgressActionAllow; got != want {
		t.Fatalf("pagerduty action = %q, want %q", got, want)
	}

	_, err = ParseCollectorEgressPolicyJSON(`{
		"mode": "broad",
		"collectors": [{"collector_kind": "pagerduty", "decision": "deny"}]
	}`)
	if err == nil {
		t.Fatal("ParseCollectorEgressPolicyJSON() error = nil, want broad-mode rule rejection")
	}
	if got, want := err.Error(), "broad collector egress policy must not include collector-specific rules"; !strings.Contains(got, want) {
		t.Fatalf("ParseCollectorEgressPolicyJSON() error = %q, want %q", got, want)
	}
}

func TestLoadConfigParsesCollectorEgressPolicy(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "active"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "true"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[{"instance_id":"pagerduty-primary","collector_kind":"pagerduty","mode":"continuous","enabled":true,"claims_enabled":true,"configuration":{"targets":[{"provider":"pagerduty","scope_id":"pagerduty:account:example","account_id":"example","token_env":"PAGERDUTY_TOKEN","incident_limit":25,"log_entry_limit":25,"change_event_limit":25}]}}]`
		case "ESHU_HOSTED_COLLECTOR_EGRESS_POLICY_JSON":
			return `{"mode":"restricted","collectors":[{"collector_kind":"pagerduty","decision":"deny"}]}`
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}

	decision := cfg.CollectorEgressPolicy.Decide(scope.CollectorPagerDuty)
	if got, want := decision.Action, CollectorEgressActionDeny; got != want {
		t.Fatalf("pagerduty action = %q, want %q", got, want)
	}
}
