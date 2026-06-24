// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestParseExtensionEgressPolicyJSONEvaluatesRestrictedRules(t *testing.T) {
	t.Parallel()

	policy, err := ParseExtensionEgressPolicyJSON(`{
		"mode": "restricted",
		"extensions": [
			{"component_id": "dev.eshu.examples.scorecard", "instance_id": "scorecard-primary", "collector_kind": "scorecard", "decision": "allow"},
			{"component_id": "dev.eshu.examples.revoked", "decision": "deny"}
		]
	}`)
	if err != nil {
		t.Fatalf("ParseExtensionEgressPolicyJSON() error = %v, want nil", err)
	}

	decision := policy.Decide(ExtensionEgressRequest{
		ComponentID:   "dev.eshu.examples.scorecard",
		InstanceID:    "scorecard-primary",
		CollectorKind: scope.CollectorKind(" scorecard "),
	})
	if got, want := decision.Action, ExtensionEgressActionAllow; got != want {
		t.Fatalf("scorecard action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, ExtensionEgressReasonAllowed; got != want {
		t.Fatalf("scorecard reason = %q, want %q", got, want)
	}

	decision = policy.Decide(ExtensionEgressRequest{
		ComponentID:   "dev.eshu.examples.revoked",
		InstanceID:    "revoked-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
	})
	if got, want := decision.Action, ExtensionEgressActionDeny; got != want {
		t.Fatalf("revoked action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, ExtensionEgressReasonDenied; got != want {
		t.Fatalf("revoked reason = %q, want %q", got, want)
	}

	decision = policy.Decide(ExtensionEgressRequest{
		ComponentID:   "dev.eshu.examples.unlisted",
		InstanceID:    "unlisted-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
	})
	if got, want := decision.Action, ExtensionEgressActionDeny; got != want {
		t.Fatalf("unlisted action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, ExtensionEgressReasonMissing; got != want {
		t.Fatalf("unlisted reason = %q, want %q", got, want)
	}
}

func TestExtensionEgressPolicyDeniesWinOverAllow(t *testing.T) {
	t.Parallel()

	policy, err := ParseExtensionEgressPolicyJSON(`{
		"mode": "restricted",
		"extensions": [
			{"component_id": "dev.eshu.examples.scorecard", "decision": "allow"},
			{"component_id": "dev.eshu.examples.scorecard", "instance_id": "scorecard-primary", "decision": "deny"}
		]
	}`)
	if err != nil {
		t.Fatalf("ParseExtensionEgressPolicyJSON() error = %v, want nil", err)
	}

	decision := policy.Decide(ExtensionEgressRequest{
		ComponentID:   "dev.eshu.examples.scorecard",
		InstanceID:    "scorecard-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
	})
	if got, want := decision.Action, ExtensionEgressActionDeny; got != want {
		t.Fatalf("scorecard action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, ExtensionEgressReasonDenied; got != want {
		t.Fatalf("scorecard reason = %q, want %q", got, want)
	}
}

func TestExtensionEgressPolicyBroadModeRequiresEmptyRules(t *testing.T) {
	t.Parallel()

	policy, err := ParseExtensionEgressPolicyJSON(`{"mode":"broad"}`)
	if err != nil {
		t.Fatalf("ParseExtensionEgressPolicyJSON() error = %v, want nil", err)
	}
	decision := policy.Decide(ExtensionEgressRequest{
		ComponentID:   "dev.eshu.examples.scorecard",
		InstanceID:    "scorecard-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
	})
	if got, want := decision.Action, ExtensionEgressActionAllow; got != want {
		t.Fatalf("scorecard action = %q, want %q", got, want)
	}

	_, err = ParseExtensionEgressPolicyJSON(`{
		"mode": "broad",
		"extensions": [{"component_id": "dev.eshu.examples.scorecard", "decision": "deny"}]
	}`)
	if err == nil {
		t.Fatal("ParseExtensionEgressPolicyJSON() error = nil, want broad-mode rule rejection")
	}
	if got, want := err.Error(), "broad extension egress policy must not include extension-specific rules"; !strings.Contains(got, want) {
		t.Fatalf("ParseExtensionEgressPolicyJSON() error = %q, want %q", got, want)
	}
}

func TestLoadConfigParsesExtensionEgressPolicy(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE":
			return "active"
		case "ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":
			return "true"
		case "ESHU_COLLECTOR_INSTANCES_JSON":
			return `[{"instance_id":"scorecard-primary","collector_kind":"scorecard","mode":"scheduled","enabled":true,"claims_enabled":true,"configuration":{"schema_version":"eshu.component.instance.v1","component_id":"dev.eshu.examples.scorecard","component_version":"0.1.0","manifest_digest":"sha256:1234","config_handle":"component-config:abcd","runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"oci"}}}]`
		case "ESHU_HOSTED_EXTENSION_EGRESS_POLICY_JSON":
			return `{"mode":"restricted","extensions":[{"component_id":"dev.eshu.examples.scorecard","decision":"deny"}]}`
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}

	decision := cfg.ExtensionEgressPolicy.Decide(ExtensionEgressRequest{
		ComponentID:   "dev.eshu.examples.scorecard",
		InstanceID:    "scorecard-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
	})
	if got, want := decision.Action, ExtensionEgressActionDeny; got != want {
		t.Fatalf("scorecard action = %q, want %q", got, want)
	}
}
