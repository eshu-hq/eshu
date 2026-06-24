// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const externalTrustRoleARN = "arn:aws:iam::111111111111:role/app"

func trustPolicyEnvelope(factID string, payload map[string]any) facts.Envelope {
	payload["role_arn"] = externalTrustRoleARN
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.AWSIAMTrustPolicyFactKind,
		Payload:  payload,
	}
}

func externalTrustObservations(envelopes ...facts.Envelope) []SecretsIAMPrivilegePostureObservation {
	return secretsIAMExternalTrustObservations(map[string][]facts.Envelope{externalTrustRoleARN: envelopes})
}

func TestExternalTrustFlagsWildcardPrincipalWithoutExternalID(t *testing.T) {
	t.Parallel()

	obs := externalTrustObservations(trustPolicyEnvelope("f-wild", map[string]any{
		"effect":            "Allow",
		"actions":           []string{"sts:AssumeRole"},
		"assume_principals": []string{"*"},
		"condition_keys":    []string{},
	}))
	if len(obs) != 1 {
		t.Fatalf("len(obs) = %d, want 1", len(obs))
	}
	if obs[0].RiskType != secretsIAMExternalTrustRiskType {
		t.Fatalf("RiskType = %q, want %q", obs[0].RiskType, secretsIAMExternalTrustRiskType)
	}
	if obs[0].Severity != "high" {
		t.Fatalf("Severity = %q, want high for wildcard", obs[0].Severity)
	}
	if obs[0].State != SecretsIAMTrustChainStatePartial {
		t.Fatalf("State = %q, want partial (provenance-only)", obs[0].State)
	}
	if len(obs[0].EvidenceFactIDs) != 1 || obs[0].EvidenceFactIDs[0] != "f-wild" {
		t.Fatalf("EvidenceFactIDs = %v, want [f-wild]", obs[0].EvidenceFactIDs)
	}
}

func TestExternalTrustFlagsCrossAccountPrincipalAtMediumSeverity(t *testing.T) {
	t.Parallel()

	obs := externalTrustObservations(trustPolicyEnvelope("f-cross", map[string]any{
		"effect":            "Allow",
		"actions":           []string{"sts:AssumeRole"},
		"assume_principals": []string{"arn:aws:iam::999999999999:root"},
		"condition_keys":    []string{},
	}))
	if len(obs) != 1 {
		t.Fatalf("len(obs) = %d, want 1", len(obs))
	}
	if obs[0].Severity != "medium" {
		t.Fatalf("Severity = %q, want medium for cross-account", obs[0].Severity)
	}
}

func TestExternalTrustFlagsBareCrossAccountIDPrincipal(t *testing.T) {
	t.Parallel()

	// AWS treats a bare 12-digit account id as identical to ::root, so it is a
	// confused-deputy vector that must be flagged the same as the ARN form.
	obs := externalTrustObservations(trustPolicyEnvelope("f-bare", map[string]any{
		"effect":            "Allow",
		"actions":           []string{"sts:AssumeRole"},
		"assume_principals": []string{"999999999999"},
		"condition_keys":    []string{},
	}))
	if len(obs) != 1 {
		t.Fatalf("len(obs) = %d, want 1 for bare cross-account id", len(obs))
	}
	if obs[0].Severity != "medium" {
		t.Fatalf("Severity = %q, want medium", obs[0].Severity)
	}
}

func TestExternalTrustIgnoresBareSameAccountIDPrincipal(t *testing.T) {
	t.Parallel()

	// 111111111111 is the role's own account (externalTrustRoleARN) — internal.
	obs := externalTrustObservations(trustPolicyEnvelope("f-bare-same", map[string]any{
		"effect":            "Allow",
		"actions":           []string{"sts:AssumeRole"},
		"assume_principals": []string{"111111111111"},
		"condition_keys":    []string{},
	}))
	if len(obs) != 0 {
		t.Fatalf("len(obs) = %d, want 0 for bare same-account id", len(obs))
	}
}

func TestExternalTrustIgnoresMitigatedAndInternalTrust(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]any{
		"same-account principal": {
			"effect": "Allow", "actions": []string{"sts:AssumeRole"},
			"assume_principals": []string{"arn:aws:iam::111111111111:role/other"}, "condition_keys": []string{},
		},
		"external id present": {
			"effect": "Allow", "actions": []string{"sts:AssumeRole"},
			"assume_principals": []string{"arn:aws:iam::999999999999:root"}, "condition_keys": []string{"sts:ExternalId"},
		},
		"wildcard mitigated by org id": {
			"effect": "Allow", "actions": []string{"sts:AssumeRole"},
			"assume_principals": []string{"*"}, "condition_keys": []string{"aws:PrincipalOrgID"},
		},
		"web identity only": {
			"effect": "Allow", "actions": []string{"sts:AssumeRoleWithWebIdentity"},
			"assume_principals": []string{"*"}, "condition_keys": []string{},
		},
		"aws service principal": {
			"effect": "Allow", "actions": []string{"sts:AssumeRole"},
			"assume_principals": []string{"ec2.amazonaws.com"}, "condition_keys": []string{},
		},
		"deny effect": {
			"effect": "Deny", "actions": []string{"sts:AssumeRole"},
			"assume_principals": []string{"*"}, "condition_keys": []string{},
		},
	}
	for name, payload := range cases {
		name, payload := name, payload
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if obs := externalTrustObservations(trustPolicyEnvelope("f", payload)); len(obs) != 0 {
				t.Fatalf("expected no observation for %q, got %d: %+v", name, len(obs), obs)
			}
		})
	}
}

func TestExternalTrustMatchesAssumeRoleWildcardActions(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"sts:*", "*"} {
		action := action
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			obs := externalTrustObservations(trustPolicyEnvelope("f", map[string]any{
				"effect": "Allow", "actions": []string{action},
				"assume_principals": []string{"arn:aws:iam::999999999999:root"}, "condition_keys": []string{},
			}))
			if len(obs) != 1 {
				t.Fatalf("action %q: len(obs) = %d, want 1", action, len(obs))
			}
		})
	}
}

func TestExternalTrustIntegratesIntoReadModels(t *testing.T) {
	t.Parallel()

	models := BuildSecretsIAMTrustChainReadModels([]facts.Envelope{
		trustPolicyEnvelope("f-int", map[string]any{
			"effect": "Allow", "actions": []string{"sts:AssumeRole"},
			"assume_principals": []string{"*"}, "condition_keys": []string{},
		}),
	})
	var found bool
	for _, obs := range models.PrivilegePostureObservations {
		if obs.RiskType == secretsIAMExternalTrustRiskType {
			found = true
		}
	}
	if !found {
		t.Fatalf("external-trust observation not present in built read models: %+v", models.PrivilegePostureObservations)
	}
}
