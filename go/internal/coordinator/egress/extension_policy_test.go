// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package egress

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestParseExtensionEgressPolicyJSONEvaluatesRestrictedRules(t *testing.T) {
	t.Parallel()

	policy, err := ParseExtensionPolicyJSON(`{
		"mode": "restricted",
		"extensions": [
			{"component_id": "dev.eshu.examples.scorecard", "instance_id": "scorecard-primary", "collector_kind": "scorecard", "decision": "allow"},
			{"component_id": "dev.eshu.examples.revoked", "decision": "deny"}
		]
	}`)
	if err != nil {
		t.Fatalf("ParseExtensionPolicyJSON() error = %v, want nil", err)
	}

	decision := policy.Decide(ExtensionRequest{
		ComponentID:   "dev.eshu.examples.scorecard",
		InstanceID:    "scorecard-primary",
		CollectorKind: scope.CollectorKind(" scorecard "),
	})
	if got, want := decision.Action, ExtensionActionAllow; got != want {
		t.Fatalf("scorecard action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, ExtensionReasonAllowed; got != want {
		t.Fatalf("scorecard reason = %q, want %q", got, want)
	}

	decision = policy.Decide(ExtensionRequest{
		ComponentID:   "dev.eshu.examples.revoked",
		InstanceID:    "revoked-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
	})
	if got, want := decision.Action, ExtensionActionDeny; got != want {
		t.Fatalf("revoked action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, ExtensionReasonDenied; got != want {
		t.Fatalf("revoked reason = %q, want %q", got, want)
	}

	decision = policy.Decide(ExtensionRequest{
		ComponentID:   "dev.eshu.examples.unlisted",
		InstanceID:    "unlisted-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
	})
	if got, want := decision.Action, ExtensionActionDeny; got != want {
		t.Fatalf("unlisted action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, ExtensionReasonMissing; got != want {
		t.Fatalf("unlisted reason = %q, want %q", got, want)
	}
}

func TestExtensionEgressPolicyDeniesWinOverAllow(t *testing.T) {
	t.Parallel()

	policy, err := ParseExtensionPolicyJSON(`{
		"mode": "restricted",
		"extensions": [
			{"component_id": "dev.eshu.examples.scorecard", "decision": "allow"},
			{"component_id": "dev.eshu.examples.scorecard", "instance_id": "scorecard-primary", "decision": "deny"}
		]
	}`)
	if err != nil {
		t.Fatalf("ParseExtensionPolicyJSON() error = %v, want nil", err)
	}

	decision := policy.Decide(ExtensionRequest{
		ComponentID:   "dev.eshu.examples.scorecard",
		InstanceID:    "scorecard-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
	})
	if got, want := decision.Action, ExtensionActionDeny; got != want {
		t.Fatalf("scorecard action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, ExtensionReasonDenied; got != want {
		t.Fatalf("scorecard reason = %q, want %q", got, want)
	}
}

func TestExtensionEgressPolicyBroadModeRequiresEmptyRules(t *testing.T) {
	t.Parallel()

	policy, err := ParseExtensionPolicyJSON(`{"mode":"broad"}`)
	if err != nil {
		t.Fatalf("ParseExtensionPolicyJSON() error = %v, want nil", err)
	}
	decision := policy.Decide(ExtensionRequest{
		ComponentID:   "dev.eshu.examples.scorecard",
		InstanceID:    "scorecard-primary",
		CollectorKind: scope.CollectorKind("scorecard"),
	})
	if got, want := decision.Action, ExtensionActionAllow; got != want {
		t.Fatalf("scorecard action = %q, want %q", got, want)
	}

	_, err = ParseExtensionPolicyJSON(`{
		"mode": "broad",
		"extensions": [{"component_id": "dev.eshu.examples.scorecard", "decision": "deny"}]
	}`)
	if err == nil {
		t.Fatal("ParseExtensionPolicyJSON() error = nil, want broad-mode rule rejection")
	}
	if got, want := err.Error(), "broad extension egress policy must not include extension-specific rules"; !strings.Contains(got, want) {
		t.Fatalf("ParseExtensionPolicyJSON() error = %q, want %q", got, want)
	}
}
